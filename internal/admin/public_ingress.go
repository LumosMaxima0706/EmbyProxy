package admin

import (
	"context"
	"crypto/tls"
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
	"sync"
	"time"

	"embyproxy/internal/spaceship"
	"embyproxy/internal/storage"
)

const publicIngressStateKey = "failover:public-ingress"
const publicIngressHistoryPrefix = "failover:public-ingress:operation:"
const automaticIngressCooldown = time.Hour

type publicIngressState struct {
	OperationID       string `json:"operation_id"`
	Phase             string `json:"phase"`
	Trigger           string `json:"trigger"`
	Mode              string `json:"mode"`
	RequestedNodeID   string `json:"requested_node_id"`
	ActiveNodeID      string `json:"active_node_id"`
	RecordName        string `json:"record_name"`
	PreviousAddress   string `json:"previous_address"`
	DesiredAddress    string `json:"desired_address"`
	ProviderAddress   string `json:"provider_address"`
	RecursiveAddress  string `json:"recursive_address"`
	RequestVerified   bool   `json:"request_verified"`
	ObservedNodeID    string `json:"observed_node_id,omitempty"`
	RollbackAttempted bool   `json:"rollback_attempted"`
	RollbackVerified  bool   `json:"rollback_verified"`
	Error             string `json:"error,omitempty"`
	StartedAt         int64  `json:"started_at"`
	CompletedAt       int64  `json:"completed_at,omitempty"`
}

type publicIngressSwitcher struct {
	mu         sync.Mutex
	h          *Handler
	record     string
	ttl        int
	resolver   *net.Resolver
	httpClient *http.Client
	now        func() time.Time
}

func newPublicIngressSwitcher(h *Handler) *publicIngressSwitcher {
	record := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.cfg.PublicIngressHost)), ".")
	return &publicIngressSwitcher{h: h, record: record, ttl: 60, resolver: net.DefaultResolver, httpClient: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}

func (s *publicIngressSwitcher) status(ctx context.Context) publicIngressState {
	var state publicIngressState
	_, _ = s.h.store.KV().GetJSON(ctx, publicIngressStateKey, &state)
	return state
}

func (s *publicIngressSwitcher) save(ctx context.Context, state publicIngressState) error {
	if err := s.h.store.KV().Put(ctx, publicIngressHistoryPrefix+state.OperationID, state); err != nil {
		return err
	}
	return s.h.store.KV().Put(ctx, publicIngressStateKey, state)
}

func (s *publicIngressSwitcher) switchTo(ctx context.Context, nodeID, trigger, mode string) (publicIngressState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Browser disconnects must not cancel a transaction after DNS changes.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Minute)
	defer cancel()
	previous := s.status(ctx)
	if publicIngressInFlight(previous.Phase) {
		return previous, errors.New("public_ingress_recovery_required")
	}
	now := s.now()
	state := publicIngressState{OperationID: fmt.Sprintf("sw-%d", now.UnixNano()), Phase: "preparing", Trigger: trigger, Mode: previous.Mode, ActiveNodeID: previous.ActiveNodeID, RequestedNodeID: nodeID, RecordName: s.record, StartedAt: now.Unix()}
	if err := s.save(ctx, state); err != nil {
		return state, err
	}
	node, err := s.h.store.GetProxyNode(ctx, nodeID)
	if err != nil || node == nil {
		return s.fail(ctx, state, "unknown_node", err)
	}
	if !eligiblePublicIngressNode(*node) {
		return s.fail(ctx, state, "target_not_eligible", nil)
	}
	ip, err := proxyNodeIPv4(*node)
	if err != nil {
		return s.fail(ctx, state, "target_address_invalid", err)
	}
	state.DesiredAddress = ip.String()
	if err = s.preflight(ctx, *node, ip); err != nil {
		return s.fail(ctx, state, "target_preflight_failed", err)
	}
	current, err := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
	if err != nil {
		return s.fail(ctx, state, "provider_read_failed", err)
	}
	state.PreviousAddress = current.Address
	if err = s.rejectIPv6(ctx); err != nil {
		return s.fail(ctx, state, "ipv6_record_requires_coordinated_switch", err)
	}
	state.Phase = "submitting_dns"
	if err = s.save(ctx, state); err != nil {
		return state, err
	}
	updated, err := s.h.dnsAutomation.ReplaceExactA(ctx, s.record, current.Address, ip.String(), s.ttl)
	if err != nil {
		observed, readErr := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
		if readErr != nil {
			state.Phase, state.Error = "recovery_required", "provider_state_unknown"
			_ = s.save(ctx, state)
			return state, errors.Join(err, readErr)
		}
		if observed.Address == ip.String() {
			return s.rollback(ctx, state, "provider_update_unverified", err)
		}
		if observed.Address != current.Address {
			state.Phase, state.Error = "recovery_required", "provider_changed_concurrently"
			_ = s.save(ctx, state)
			return state, err
		}
		return s.fail(ctx, state, "provider_update_failed", err)
	}
	state.ProviderAddress = updated.Address
	state.Phase = "waiting_recursive"
	if err = s.save(ctx, state); err != nil {
		return s.rollback(ctx, state, "state_persist_failed", err)
	}
	observed, err := s.waitRecursive(ctx, ip.String(), 90*time.Second)
	state.RecursiveAddress = observed
	if err != nil {
		return s.rollback(ctx, state, "recursive_verification_failed", err)
	}
	state.Phase = "verifying_request"
	if err = s.save(ctx, state); err != nil {
		return s.rollback(ctx, state, "state_persist_failed", err)
	}
	observedNode, verifyErr := s.verifyPublicRequest(ctx, nodeID)
	state.ObservedNodeID = observedNode
	if err = verifyErr; err != nil {
		return s.rollback(ctx, state, "public_request_failed", err)
	}
	state.RequestVerified = true
	state.Mode = mode
	state.ActiveNodeID = nodeID
	state.Phase = "verified"
	state.CompletedAt = s.now().Unix()
	state.Error = ""
	if err = s.save(ctx, state); err != nil {
		return state, err
	}
	return state, nil
}

func publicIngressInFlight(phase string) bool {
	switch phase {
	case "preparing", "submitting_dns", "waiting_recursive", "verifying_request", "rolling_back", "recovery_required", "rollback_failed":
		return true
	}
	return false
}

func (s *publicIngressSwitcher) rejectIPv6(ctx context.Context) error {
	records, err := s.h.dnsAutomation.List(ctx, s.h.dnsAutomation.ManagedDomain)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(s.record, "."+strings.TrimSuffix(strings.ToLower(s.h.dnsAutomation.ManagedDomain), "."))
	for _, record := range records {
		got := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record.Name)), ".")
		if (got == name || got == s.record) && strings.EqualFold(record.Type, "AAAA") {
			return errors.New("public_ingress_ipv6_record_present")
		}
	}
	return nil
}

func eligiblePublicIngressNode(n storage.ProxyNode) bool {
	return n.Enabled && n.State == "healthy" && n.PlaybackHealthy && n.IngressHealthy && n.ConfigSynced && n.LastHeartbeatAt > time.Now().Add(-5*time.Minute).Unix()
}

func proxyNodeIPv4(n storage.ProxyNode) (netip.Addr, error) {
	if n.DNSExpectedIP != "" {
		if ip, e := netip.ParseAddr(n.DNSExpectedIP); e == nil && ip.Is4() {
			return ip, nil
		}
	}
	u, e := url.Parse(n.PublicAddress)
	if e != nil {
		return netip.Addr{}, e
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && ip.Is4() {
		return ip, nil
	}
	ips, e := net.DefaultResolver.LookupNetIP(context.Background(), "ip4", u.Hostname())
	if e != nil {
		return netip.Addr{}, e
	}
	for _, ip := range ips {
		if ip.Is4() {
			return ip, nil
		}
	}
	return netip.Addr{}, errors.New("no_ipv4")
}

func (s *publicIngressSwitcher) preflight(ctx context.Context, n storage.ProxyNode, ip netip.Addr) error {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{ServerName: s.record, MinVersion: tls.VersionTLS12}, DialContext: func(c context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(c, network, net.JoinHostPort(ip.String(), "443"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+s.record+"/health", nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health_status_%d", resp.StatusCode)
	}
	if strings.TrimSpace(resp.Header.Get("X-EmbyProxy-Node-ID")) != n.ID {
		return errors.New("preflight_node_identity_mismatch")
	}
	paths := make([]string, 0, len(s.h.cfg.PublicMediaNodePaths)+len(s.h.cfg.PublicMediaExtraPaths))
	for _, path := range s.h.cfg.PublicMediaNodePaths {
		paths = append(paths, strings.TrimRight(path, "/")+"/System/Info/Public")
	}
	for _, path := range s.h.cfg.PublicMediaExtraPaths {
		paths = append(paths, strings.TrimRight(path, "/")+"/System/Info/Public")
	}
	if len(paths) == 0 {
		return errors.New("preflight_public_routes_unconfigured")
	}
	sort.Strings(paths)
	for _, path := range paths {
		route, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+s.record+path, nil)
		if err != nil {
			return err
		}
		routeResp, err := client.Do(route)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(routeResp.Body, 4096))
		_ = routeResp.Body.Close()
		if routeResp.StatusCode != http.StatusOK {
			return fmt.Errorf("preflight_route_status_%d", routeResp.StatusCode)
		}
	}
	return nil
}

func (s *publicIngressSwitcher) waitRecursive(ctx context.Context, want string, timeout time.Duration) (string, error) {
	deadline := s.now().Add(timeout)
	var last string
	for s.now().Before(deadline) {
		ips, err := s.resolver.LookupHost(ctx, s.record)
		if err == nil {
			for _, v := range ips {
				if ip, e := netip.ParseAddr(v); e == nil && ip.Is4() {
					last = ip.String()
					if last == want {
						return last, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return last, errors.New("recursive_timeout")
}

func (s *publicIngressSwitcher) verifyPublicRequest(ctx context.Context, expectedNodeID string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+s.record+"/health", nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	observed := strings.TrimSpace(resp.Header.Get("X-EmbyProxy-Node-ID"))
	if resp.StatusCode != http.StatusOK {
		return observed, fmt.Errorf("public_status_%d", resp.StatusCode)
	}
	if observed != expectedNodeID {
		return observed, errors.New("public_node_identity_mismatch")
	}
	return observed, nil
}

func (s *publicIngressSwitcher) rollback(ctx context.Context, state publicIngressState, code string, cause error) (publicIngressState, error) {
	state.RollbackAttempted = true
	state.Phase = "rolling_back"
	state.Error = code
	_ = s.save(ctx, state)
	current, readErr := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
	if readErr == nil && current.Address != state.DesiredAddress && current.Address != state.PreviousAddress {
		readErr = errors.New("rollback_record_changed_concurrently")
	}
	if readErr == nil && current.Address == state.DesiredAddress && state.PreviousAddress != "" {
		_, readErr = s.h.dnsAutomation.ReplaceExactA(ctx, s.record, state.DesiredAddress, state.PreviousAddress, s.ttl)
	}
	if readErr == nil {
		_, readErr = s.waitRecursive(ctx, state.PreviousAddress, 90*time.Second)
		if readErr == nil {
			current, readErr = s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
			if readErr == nil && current.Address != state.PreviousAddress {
				readErr = errors.New("rollback_provider_readback_mismatch")
			}
		}
	}
	state.RollbackVerified = readErr == nil
	state.Phase = map[bool]string{true: "rolled_back", false: "rollback_failed"}[state.RollbackVerified]
	state.CompletedAt = s.now().Unix()
	_ = s.save(ctx, state)
	if cause == nil {
		cause = errors.New(code)
	}
	if readErr != nil {
		return state, errors.Join(cause, readErr)
	}
	return state, cause
}
func (s *publicIngressSwitcher) fail(ctx context.Context, state publicIngressState, code string, cause error) (publicIngressState, error) {
	state.Phase = "failed"
	state.Error = code
	state.CompletedAt = s.now().Unix()
	_ = s.save(ctx, state)
	if cause == nil {
		cause = errors.New(code)
	}
	return state, cause
}

func writePublicIngressState(w http.ResponseWriter, status int, state publicIngressState, err error) {
	body := map[string]any{"ok": err == nil, "operation": state}
	if err != nil {
		body["error"] = state.Error
	}
	writeJSON(w, status, body)
}
func decodePublicIngressRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var b struct {
		NodeID  string `json:"node_id"`
		Mode    string `json:"mode"`
		Confirm bool   `json:"confirm"`
	}
	if !decodeAuthJSON(w, r, &b) {
		return "", "", false
	}
	if !b.Confirm || strings.TrimSpace(b.NodeID) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "CONFIRMATION_REQUIRED"})
		return "", "", false
	}
	if b.Mode == "" {
		b.Mode = "preferred"
	}
	if b.Mode != "fixed" && b.Mode != "preferred" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "INVALID_MODE"})
		return "", "", false
	}
	return strings.TrimSpace(b.NodeID), b.Mode, true
}

var _ = json.Marshal

// reconcile applies automatic health/traffic policy through the exact same
// switch transaction used by an administrator. Fixed mode is never overridden.
func (s *publicIngressSwitcher) reconcile(ctx context.Context) error {
	state := s.status(ctx)
	if publicIngressInFlight(state.Phase) {
		return errors.New("public_ingress_recovery_required")
	}
	if state.Mode == "fixed" {
		return nil
	}
	if state.Phase != "verified" && state.Phase != "rolled_back" {
		return errors.New("public_ingress_not_initialized_or_verified")
	}
	// Automatic switching is delayed after a verified operation; manual requests are not.
	nodes, err := s.h.store.ListProxyNodes(ctx)
	if err != nil {
		return err
	}
	var active *storage.ProxyNode
	for i := range nodes {
		if nodes[i].ID == state.ActiveNodeID {
			active = &nodes[i]
			break
		}
	}
	needsSwitch := active == nil || !eligiblePublicIngressNode(*active)
	trigger := "automatic_health"
	if !needsSwitch && state.Phase == "verified" && s.now().Sub(time.Unix(state.CompletedAt, 0)) < automaticIngressCooldown {
		return nil
	}
	if !needsSwitch && active != nil && active.QuotaBytes > 0 && active.ThresholdPercent > 0 && float64(active.UsedBytes)*100 >= float64(active.QuotaBytes)*active.ThresholdPercent {
		needsSwitch = true
		trigger = "automatic_threshold"
	}
	if !needsSwitch {
		return nil
	}
	for i := range nodes {
		candidate := nodes[i]
		if candidate.ID == state.ActiveNodeID || !eligiblePublicIngressNode(candidate) || (candidate.QuotaBytes > 0 && candidate.ThresholdPercent > 0 && float64(candidate.UsedBytes)*100 >= float64(candidate.QuotaBytes)*candidate.ThresholdPercent) {
			continue
		}
		_, err = s.switchTo(ctx, candidate.ID, trigger, "preferred")
		return err
	}
	return errors.New("no_eligible_public_ingress_candidate")
}

func (h *Handler) StartPublicIngressScheduler(ctx context.Context) {
	if h == nil || h.publicIngress == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = h.publicIngress.reconcile(ctx)
			}
		}
	}()
}

// SetDNSAutomationClient is intentionally narrow and is used by isolated
// integration tests; production initializes the same client from encrypted KV.
func (h *Handler) SetDNSAutomationClient(client *spaceship.Client) {
	h.dnsAutomation = client
	if client != nil {
		h.publicIngress = newPublicIngressSwitcher(h)
	} else {
		h.publicIngress = nil
	}
}
