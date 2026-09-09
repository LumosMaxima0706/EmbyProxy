package spaceship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Client is the minimal Controller-side Spaceship DNS transport. Credentials
// are held only in memory and are never serialized into edge configuration.
type Client struct {
	BaseURL, APIKey, APISecret, ManagedDomain string
	HTTPClient                                *http.Client
}
type Record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Address string `json:"address"`
	TTL     int    `json:"ttl"`
}
type recordsResponse struct {
	Records []Record `json:"records"`
	Items   []Record `json:"items"`
}
type recordsWriteRequest struct {
	Force bool     `json:"force"`
	Items []Record `json:"items"`
}
type HTTPError struct {
	Status int
	Detail string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("spaceship_http_%d", e.Status) }

func safeDetail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var obj struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
		Title   string `json:"title"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil {
		for _, value := range []string{obj.Detail, obj.Message, obj.Title} {
			if value != "" {
				raw = value
				break
			}
		}
	}
	if len(raw) > 256 {
		raw = raw[:256]
	}
	if strings.ContainsAny(raw, "\x00\r\n") {
		raw = strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == 0 {
				return ' '
			}
			return r
		}, raw)
	}
	return raw
}

func (c *Client) enabled() bool {
	return c != nil && c.APIKey != "" && c.APISecret != "" && c.ManagedDomain != ""
}
func (c *Client) endpoint(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	if managed == "" || (domain != managed && !strings.HasSuffix(domain, "."+managed)) {
		return "", errors.New("managed_domain_not_allowed")
	}
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = "https://spaceship.dev/api"
	}
	return base + "/v1/dns/records/" + url.PathEscape(domain), nil
}
func (c *Client) do(ctx context.Context, method, domain string, body any, out any) error {
	if !c.enabled() {
		return errors.New("spaceship_not_configured")
	}
	ep, err := c.endpoint(domain)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, ep, rd)
	if err != nil {
		return err
	}
	if method == http.MethodGet {
		q := req.URL.Query()
		q.Set("take", "100")
		q.Set("skip", "0")
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("X-API-Secret", c.APISecret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{Status: resp.StatusCode, Detail: safeDetail(string(detail))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}
func (c *Client) List(ctx context.Context, domain string) ([]Record, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, domain, nil, &raw); err != nil {
		return nil, err
	}
	var wrapped recordsResponse
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Records != nil {
		return wrapped.Records, nil
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Items != nil {
		return wrapped.Items, nil
	}
	var list []Record
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}
func (c *Client) Test(ctx context.Context) error { _, err := c.List(ctx, c.ManagedDomain); return err }
func (c *Client) TestStatus(ctx context.Context) (int, string, error) {
	_, err := c.List(ctx, c.ManagedDomain)
	if err == nil {
		return http.StatusOK, "", nil
	}
	if e, ok := err.(*HTTPError); ok {
		return e.Status, e.Detail, err
	}
	return 0, "network or TLS error", err
}

func (c *Client) AuthoritativeProvider(ctx context.Context) (string, []string) {
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	if managed == "" {
		return "Unknown", nil
	}
	resolver := net.DefaultResolver
	ns, err := resolver.LookupNS(ctx, managed)
	if err != nil || len(ns) == 0 {
		return "Unknown", nil
	}
	values := make([]string, 0, len(ns))
	spaceshipNS := true
	for _, item := range ns {
		host := strings.TrimSuffix(strings.ToLower(item.Host), ".")
		values = append(values, host)
		if !strings.Contains(host, "spaceship") {
			spaceshipNS = false
		}
	}
	if spaceshipNS {
		return "Spaceship", values
	}
	return "Custom", values
}
func (c *Client) EnsureA(ctx context.Context, prefix string, ip netip.Addr, ttl int) (Record, error) {
	if ttl == 0 {
		ttl = 300
	}
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" || len(prefix) > 63 || strings.ContainsAny(prefix, "._/\\?# \t\r\n") || strings.HasPrefix(prefix, "-") || strings.HasSuffix(prefix, "-") {
		return Record{}, errors.New("invalid_dns_prefix")
	}
	if !ip.Is4() {
		return Record{}, errors.New("invalid_ipv4")
	}
	if ttl < 30 || ttl > 86400 {
		return Record{}, errors.New("invalid_dns_ttl")
	}
	domain := strings.TrimSuffix(prefix+"."+c.ManagedDomain, ".")
	records, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return Record{}, err
	}
	var same *Record
	for i := range records {
		r := records[i]
		name := strings.TrimSuffix(strings.ToLower(r.Name), ".")
		if name == prefix || name == domain {
			if r.Type != "A" {
				return Record{}, errors.New("dns_record_conflict")
			}
			if r.Address == ip.String() {
				rr := r
				same = &rr
			} else {
				rr := r
				same = &rr
			}
		}
	}
	if same != nil && same.Address == ip.String() {
		return *same, nil
	}
	if same != nil {
		return Record{}, errors.New("dns_record_conflict")
	}
	if err := c.putRecords(ctx, c.ManagedDomain, []Record{{Name: prefix, Type: "A", Address: ip.String(), TTL: ttl}}); err != nil {
		return Record{}, err
	}
	verified, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return Record{}, err
	}
	for _, r := range verified {
		name := strings.TrimSuffix(strings.ToLower(r.Name), ".")
		if (name == prefix || name == domain) && r.Type == "A" && r.Address == ip.String() {
			return r, nil
		}
	}
	return Record{}, errors.New("dns_record_not_verified")
}
func (c *Client) putRecords(ctx context.Context, domain string, records []Record) error {
	return c.do(ctx, http.MethodPut, domain, recordsWriteRequest{Force: true, Items: records}, nil)
}
