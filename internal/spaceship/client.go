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
	"sort"
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
	Address string `json:"address,omitempty"`
	Value   string `json:"value,omitempty"`
	TTL     int    `json:"ttl"`
}
type recordsResponse struct {
	Records []Record `json:"records"`
	Items   []Record `json:"items"`
	Total   int      `json:"total"`
}
type recordsWriteRequest struct {
	Force bool     `json:"force"`
	Items []Record `json:"items"`
}
type recordDeleteRequest struct {
	Value   string `json:"value,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
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
		q.Set("take", "500")
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
		if wrapped.Total > len(wrapped.Items) {
			return nil, errors.New("spaceship_records_page_incomplete")
		}
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

// ExactRecord returns exactly one managed DNS record. Both relative and FQDN
// names are accepted from the provider, but ambiguous duplicates fail closed.
func (c *Client) ExactRecord(ctx context.Context, fqdn, recordType string) (Record, error) {
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if managed == "" || fqdn == managed || !strings.HasSuffix(fqdn, "."+managed) {
		return Record{}, errors.New("managed_record_not_allowed")
	}
	name := strings.TrimSuffix(fqdn, "."+managed)
	records, err := c.List(ctx, managed)
	if err != nil {
		return Record{}, err
	}
	var matches []Record
	for _, record := range records {
		recordName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record.Name)), ".")
		if (recordName == name || recordName == fqdn) && strings.EqualFold(record.Type, recordType) {
			matches = append(matches, record)
		}
	}
	if len(matches) != 1 {
		return Record{}, errors.New("managed_record_match_not_unique")
	}
	return matches[0], nil
}

// ReplaceExactA updates one existing A record and verifies the provider
// readback. It never creates a new name and never mutates unrelated records.
func (c *Client) ReplaceExactA(ctx context.Context, fqdn, expectedPrevious, address string, ttl int) (Record, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil || !addr.Is4() {
		return Record{}, errors.New("invalid_ipv4")
	}
	if ttl < 30 || ttl > 86400 {
		return Record{}, errors.New("invalid_dns_ttl")
	}
	before, err := c.ExactRecord(ctx, fqdn, "A")
	if err != nil {
		return Record{}, err
	}
	if expectedPrevious != "" && before.Address != expectedPrevious {
		return Record{}, errors.New("managed_record_compare_and_swap_failed")
	}
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	name := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), "."), "."+managed)
	if err := c.putRecords(ctx, managed, []Record{{Name: name, Type: "A", Address: addr.String(), TTL: ttl}}); err != nil {
		return Record{}, err
	}
	after, err := c.ExactRecord(ctx, fqdn, "A")
	if err != nil {
		return Record{}, err
	}
	if after.Address != addr.String() || after.TTL != ttl {
		return Record{}, errors.New("managed_record_update_not_verified")
	}
	return after, nil
}

func (c *Client) streamACMEName(host, value string) (string, error) {
	if c == nil {
		return "", errors.New("spaceship_not_configured")
	}
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	if managed == "" || strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".") != "stream."+managed {
		return "", errors.New("acme_domain_not_allowed")
	}
	if len(value) < 32 || len(value) > 128 {
		return "", errors.New("invalid_acme_challenge")
	}
	for _, ch := range value {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return "", errors.New("invalid_acme_challenge")
		}
	}
	return "_acme-challenge.stream", nil
}

func streamACMEMatch(r Record, name, managed string) bool {
	value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(r.Name)), ".")
	return value == name || value == name+"."+strings.TrimSuffix(strings.ToLower(managed), ".")
}

// PresentStreamACME writes only the managed stream DNS-01 TXT value.
func (c *Client) PresentStreamACME(ctx context.Context, host, value string) error {
	name, err := c.streamACMEName(host, value)
	if err != nil {
		return err
	}
	before, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return err
	}
	count := 0
	for _, r := range before {
		if streamACMEMatch(r, name, c.ManagedDomain) && r.Type == "TXT" && r.Value == value {
			count++
		}
	}
	if count > 1 {
		return errors.New("acme_challenge_not_unique")
	}
	if count == 0 {
		if err := c.putRecords(ctx, c.ManagedDomain, []Record{{Name: name, Type: "TXT", Value: value, TTL: 60}}); err != nil {
			return err
		}
	}
	after, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return err
	}
	count = 0
	for _, r := range after {
		if streamACMEMatch(r, name, c.ManagedDomain) && r.Type == "TXT" && r.Value == value {
			count++
		}
	}
	if count != 1 {
		return errors.New("acme_challenge_not_verified")
	}
	return nil
}

// CleanupStreamACME removes one matching TXT value without touching other records.
func (c *Client) CleanupStreamACME(ctx context.Context, host, value string) error {
	name, err := c.streamACMEName(host, value)
	if err != nil {
		return err
	}
	before, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return err
	}
	var matching []Record
	unrelated := make([]Record, 0, len(before))
	for _, r := range before {
		if streamACMEMatch(r, name, c.ManagedDomain) && r.Type == "TXT" && r.Value == value {
			matching = append(matching, r)
		} else {
			unrelated = append(unrelated, r)
		}
	}
	if len(matching) == 0 {
		return nil
	}
	if len(matching) != 1 {
		return errors.New("acme_challenge_not_unique")
	}
	if err := c.do(ctx, http.MethodDelete, c.ManagedDomain, []recordDeleteRequest{{Name: name, Type: "TXT", Value: value}}, nil); err != nil {
		return err
	}
	after, err := c.List(ctx, c.ManagedDomain)
	if err != nil {
		return err
	}
	if !recordsEqual(unrelated, after) {
		return errors.New("acme_challenge_cleanup_not_verified")
	}
	return nil
}

// DeleteExact removes one verified managed record. Spaceship list responses do
// not always include record IDs, so deletion uses its exact name/type/address
// API and verifies that no unrelated record changed.
func (c *Client) DeleteExact(ctx context.Context, fqdn, recordType, address string) error {
	managed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.ManagedDomain)), ".")
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if managed == "" || fqdn == managed || !strings.HasSuffix(fqdn, "."+managed) {
		return errors.New("managed_record_not_allowed")
	}
	addr, parseErr := netip.ParseAddr(address)
	if recordType != "A" || parseErr != nil || !addr.Is4() {
		return errors.New("managed_record_identity_invalid")
	}
	name := strings.TrimSuffix(fqdn, "."+managed)
	before, err := c.List(ctx, managed)
	if err != nil {
		return err
	}
	matches := make([]Record, 0, 1)
	unrelated := make([]Record, 0, len(before))
	for _, record := range before {
		recordName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record.Name)), ".")
		if (recordName == name || recordName == fqdn) && record.Type == recordType {
			matches = append(matches, record)
		} else {
			unrelated = append(unrelated, record)
		}
	}
	if len(matches) != 1 {
		return errors.New("managed_record_match_not_unique")
	}
	if matches[0].Address != address {
		return errors.New("managed_record_address_changed")
	}
	payload := []recordDeleteRequest{{Name: name, Type: recordType, Address: address}}
	if err := c.do(ctx, http.MethodDelete, managed, payload, nil); err != nil {
		return err
	}
	after, err := c.List(ctx, managed)
	if err != nil {
		return err
	}
	for _, record := range after {
		recordName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record.Name)), ".")
		if (recordName == name || recordName == fqdn) && record.Type == recordType {
			return errors.New("managed_record_delete_not_verified")
		}
	}
	if !recordsEqual(unrelated, after) {
		return errors.New("managed_record_unrelated_changed")
	}
	return nil
}

func recordsEqual(a, b []Record) bool {
	if len(a) != len(b) {
		return false
	}
	encode := func(values []Record) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			raw, _ := json.Marshal(value)
			result = append(result, string(raw))
		}
		sort.Strings(result)
		return result
	}
	aa, bb := encode(a), encode(b)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
