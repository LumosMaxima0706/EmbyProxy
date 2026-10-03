package admin

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"embyproxy/internal/spaceship"
)

type ingressReadback struct {
	provider *spaceship.Client
	record   string
	lookup   func(context.Context, string) ([]string, error)
	client   *http.Client
}
type ingressReadbackResult struct {
	Provider     ingressProviderView `json:"provider"`
	Recursive    []string            `json:"recursive,omitempty"`
	PublicStatus int                 `json:"public_status,omitempty"`
	PublicNodeID string              `json:"public_node_id,omitempty"`
	Errors       []string            `json:"errors,omitempty"`
	ObservedAt   int64               `json:"observed_at"`
}

func (o ingressReadback) read(ctx context.Context) ingressReadbackResult {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	result := ingressReadbackResult{ObservedAt: time.Now().Unix()}
	if o.provider == nil {
		result.Errors = append(result.Errors, "provider_unavailable")
	} else {
		records, err := o.provider.List(ctx, o.provider.ManagedDomain)
		if err != nil {
			result.Errors = append(result.Errors, "provider_read_failed")
		} else {
			result.Provider = providerIngressRecord(records, o.record, o.provider.ManagedDomain)
		}
		if result.Provider.Error != "" {
			result.Errors = append(result.Errors, result.Provider.Error)
		}
	}
	if o.lookup == nil {
		result.Errors = append(result.Errors, "recursive_unavailable")
	} else {
		answers, err := o.lookup(ctx, o.record)
		if err != nil || len(answers) == 0 {
			result.Errors = append(result.Errors, "recursive_lookup_failed")
		}
		for _, answer := range answers {
			if ip, e := netip.ParseAddr(answer); e == nil {
				result.Recursive = append(result.Recursive, ip.String())
			} else {
				result.Errors = append(result.Errors, "recursive_answer_invalid")
			}
		}
	}
	if result.Provider.Address != "" {
		for _, ip := range result.Recursive {
			if ip != result.Provider.Address {
				result.Errors = append(result.Errors, "recursive_provider_mismatch")
				break
			}
		}
	}
	if o.client == nil {
		result.Errors = append(result.Errors, "public_client_unavailable")
	} else {
		switcher := &publicIngressSwitcher{record: o.record, httpClient: o.client, lookupHost: func(context.Context, string) ([]string, error) { return result.Recursive, nil }}
		client, closeIdle, err := switcher.freshPublicRequestClient(ctx)
		if err != nil {
			result.Errors = append(result.Errors, "public_request_failed")
			return result
		}
		defer closeIdle()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+o.record+"/health", nil)
		req.Close = true
		resp, err := client.Do(req)
		if err != nil {
			result.Errors = append(result.Errors, "public_request_failed")
		} else {
			result.PublicStatus = resp.StatusCode
			result.PublicNodeID = strings.TrimSpace(resp.Header.Get("X-EmbyProxy-Node-ID"))
			if len(result.PublicNodeID) > 96 || strings.ContainsAny(result.PublicNodeID, " \t\r\n/\\") {
				result.PublicNodeID = ""
				result.Errors = append(result.Errors, "public_identity_invalid")
			}
			if result.PublicNodeID == "" {
				result.Errors = append(result.Errors, "public_identity_missing")
			}
			if resp.StatusCode != http.StatusOK {
				result.Errors = append(result.Errors, "public_status_unexpected")
			}
			_ = resp.Body.Close()
		}
	}
	return result
}
