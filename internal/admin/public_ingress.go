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
const publicIngressSchedulerKey = "failover:public-ingress:scheduler"
const publicIngressHistoryPrefix = "failover:public-ingress:operation:"
const automaticIngressCooldown = time.Hour

type publicIngressState struct {
	OperationID           string `json:"operation_id"`
	PriorVerifiedID       string `json:"prior_verified_id,omitempty"`
	Phase                 string `json:"phase"`
	Trigger               string `json:"trigger"`
	Mode                  string `json:"mode"`
	RequestedNodeID       string `json:"requested_node_id"`
	ActiveNodeID          string `json:"active_node_id"`
	RecordName            string `json:"record_name"`
	PreviousAddress       string `json:"previous_address"`
	RecoverySourceAddress string `json:"recovery_source_address,omitempty"`
	DesiredAddress        string `json:"desired_address"`
	ProviderAddress       string `json:"provider_address"`
	RecursiveAddress      string `json:"recursive_address"`
	RequestVerified       bool   `json:"request_verified"`
	ObservedNodeID        string `json:"observed_node_id,omitempty"`
	RollbackAttempted     bool   `json:"rollback_attempted"`
	RollbackVerified      bool   `json:"rollback_verified"`
	Error                 string `json:"error,omitempty"`
	RequestError          string `json:"request_error,omitempty"`
	FailedObservedNodeID  string `json:"failed_observed_node_id,omitempty"`
	StartedAt             int64  `json:"started_at"`
	CompletedAt           int64  `json:"completed_at,omitempty"`
}

type publicIngressSchedulerStatus struct {
	LastAttemptAt int64  `json:"last_attempt_at,omitempty"`
	LastSuccessAt int64  `json:"last_success_at,omitempty"`
	NextRunAt     int64  `json:"next_run_at,omitempty"`
	Error         string `json:"error,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

type publicIngressSwitcher struct {
	mu         sync.Mutex
	h          *Handler
	record     string
	ttl        int
	lookupIP   func(context.Context, string, string) ([]netip.Addr, error)
	lookupHost func(context.Context, string) ([]string, error)
	httpClient *http.Client
	now        func() time.Time
}

func newPublicIngressSwitcher(h *Handler) *publicIngressSwitcher {
	record := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.cfg.PublicIngressHost)), ".")
	return &publicIngressSwitcher{h: h, record: record, ttl: 60, lookupHost: lookupPublicA, lookupIP: net.DefaultResolver.LookupNetIP, httpClient: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}

func lookupPublicA(ctx context.Context, host string) ([]string, error) {
	return lookupPublicAEndpoints(ctx, []string{
		"https://dns.google/resolve?name=" + url.QueryEscape(host) + "&type=A",
		"https://cloudflare-dns.com/dns-query?name=" + url.QueryEscape(host) + "&type=A",
	})
}

func lookupPublicAEndpoints(ctx context.Context, endpoints []string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var consensus []string
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/dns-json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var document struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&document)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil || document.Status != 0 {
			return nil, errors.New("public_recursive_lookup_failed")
		}
		answers := make([]string, 0, len(document.Answer))
		for _, answer := range document.Answer {
			if answer.Type == 1 {
				ip, err := netip.ParseAddr(strings.TrimSpace(answer.Data))
				if err != nil || !ip.Is4() {
					return nil, errors.New("public_recursive_answer_invalid")
				}
				answers = append(answers, ip.String())
			}
		}
		sort.Strings(answers)
		if len(answers) == 0 {
			return nil, errors.New("public_recursive_answer_empty")
		}
		if consensus != nil && strings.Join(consensus, ",") != strings.Join(answers, ",") {
			return nil, errors.New("public_recursive_resolvers_disagree")
		}
		consensus = answers
	}
	return consensus, nil
}

func (s *publicIngressSwitcher) status(ctx context.Context) publicIngressState {
	var state publicIngressState
	raw, found, err := s.h.store.KV().Get(ctx, publicIngressStateKey)
	if err != nil || (found && (json.Unmarshal([]byte(raw), &state) != nil || state.OperationID == "" || state.Phase == "")) {
		return publicIngressState{Phase: "recovery_required", Error: "public_ingress_state_unreadable"}
	}
	return state
}

func (s *publicIngressSwitcher) schedulerStatus(ctx context.Context) publicIngressSchedulerStatus {
	var status publicIngressSchedulerStatus
	_, _ = s.h.store.KV().GetJSON(ctx, publicIngressSchedulerKey, &status)
	return status
}

func (s *publicIngressSwitcher) saveSchedulerStatus(ctx context.Context, code, trigger string) error {
	status := s.schedulerStatus(ctx)
	status.Error, status.Trigger, status.UpdatedAt = code, trigger, s.now().Unix()
	return s.h.store.KV().Put(ctx, publicIngressSchedulerKey, status)
}

func (s *publicIngressSwitcher) save(ctx context.Context, state publicIngressState) error {
	return s.h.store.KV().PutJSONPair(ctx, publicIngressHistoryPrefix+state.OperationID, publicIngressStateKey, state)
}

func (s *publicIngressSwitcher) switchTo(ctx context.Context, nodeID, trigger, mode string) (publicIngressState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switchToLocked(ctx, nodeID, trigger, mode)
}

func (s *publicIngressSwitcher) switchToLocked(ctx context.Context, nodeID, trigger, mode string) (publicIngressState, error) {
	// Browser disconnects must not cancel a transaction after DNS changes.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Minute)
	defer cancel()
	previous := s.status(ctx)
	if publicIngressInFlight(previous.Phase) {
		return previous, errors.New("public_ingress_recovery_required")
	}
	now := s.now()
	state := publicIngressState{OperationID: fmt.Sprintf("sw-%d", now.UnixNano()), Phase: "preparing", Trigger: trigger, Mode: previous.Mode, ActiveNodeID: previous.ActiveNodeID, RequestedNodeID: nodeID, RecordName: s.record, StartedAt: now.Unix()}
	if previous.Phase == "verified" && previous.RequestVerified && previous.ActiveNodeID != "" && previous.DesiredAddress != "" {
		state.PriorVerifiedID = previous.OperationID
	}
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
	ip, err := s.proxyNodeIPv4(ctx, *node)
	if err != nil {
		return s.fail(ctx, state, "target_address_invalid", err)
	}
	state.DesiredAddress = ip.String()
	if err = s.preflight(ctx, *node, ip); err != nil {
		return s.fail(ctx, state, "target_preflight_failed", err)
	}
	if err = s.runtimeMediaPreflight(ctx, *node); err != nil {
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
	observedNode, verifyErr := s.waitPublicRequest(ctx, nodeID, 30*time.Second)
	state.ObservedNodeID = observedNode
	if err = verifyErr; err != nil {
		state.RequestError = publicRequestErrorCode(err)
		state.FailedObservedNodeID = observedNode
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
	_ = s.saveSchedulerStatus(ctx, "", "")
	return state, nil
}

func publicIngressInFlight(phase string) bool {
	switch phase {
	case "preparing", "submitting_dns", "waiting_recursive", "verifying_request", "rolling_back", "recovery_required", "rollback_failed", "failover_pending":
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
	managed, err := s.h.store.ListManagedRoutes(ctx)
	if err != nil {
		return errors.New("preflight_route_list_failed")
	}
	for _, route := range managed {
		if route.Enabled && route.Public {
			paths = append(paths, "/s/"+url.PathEscape(route.Slug)+"/emby/System/Info/Public")
		}
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
		route.Header.Set("User-Agent", "Yamby")
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
		ips, err := s.lookupHost(ctx, s.record)
		if err == nil && len(ips) != 0 {
			for _, v := range ips {
				if ip, e := netip.ParseAddr(v); e == nil && ip.Is4() {
					last = ip.String()
				}
			}
			if recursiveAnswersMatch(ips, want) {
				return want, nil
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

func recursiveAnswersMatch(ips []string, want string) bool {
	if len(ips) == 0 {
		return false
	}
	for _, text := range ips {
		ip, err := netip.ParseAddr(text)
		if err != nil || !ip.Is4() || ip.String() != want {
			return false
		}
	}
	return true
}

func (s *publicIngressSwitcher) freshPublicRequestClient(ctx context.Context) (*http.Client, func(), error) {
	base := s.httpClient
	if base == nil {
		base = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	client := *base
	var transport *http.Transport
	if configured, ok := base.Transport.(*http.Transport); ok {
		transport = configured.Clone()
	} else if base.Transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	if transport == nil {
		return &client, func() {}, nil
	}
	transport.DisableKeepAlives = true
	if base.Transport == nil {
		answers, err := s.lookupHost(ctx, s.record)
		if err != nil || len(answers) == 0 {
			return nil, func() {}, errors.New("public_recursive_lookup_failed")
		}
		ip, err := netip.ParseAddr(answers[0])
		if err != nil || !ip.Is4() || !recursiveAnswersMatch(answers, ip.String()) {
			return nil, func() {}, errors.New("public_recursive_answer_ambiguous")
		}
		transport.Proxy = nil
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		transport.DialContext = func(c context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(c, network, net.JoinHostPort(ip.String(), "443"))
		}
	}
	client.Transport = transport
	return &client, transport.CloseIdleConnections, nil
}

func (s *publicIngressSwitcher) verifyPublicRequest(ctx context.Context, expectedNodeID string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+s.record+"/health", nil)
	req.Close = true
	client, closeIdle, err := s.freshPublicRequestClient(ctx)
	if err != nil {
		return "", err
	}
	defer closeIdle()
	resp, err := client.Do(req)
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

func publicRequestErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if strings.HasPrefix(err.Error(), "public_") {
		return err.Error()
	}
	var certificateError *tls.CertificateVerificationError
	if errors.As(err, &certificateError) {
		return "public_certificate_verification_failed"
	}
	return "public_transport_failed"
}

// A first DNS consensus can be followed by a stale anycast cache response.
// Keep each attempt fresh and identity-checked before declaring failure.
func (s *publicIngressSwitcher) waitPublicRequest(ctx context.Context, nodeID string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var observed string
	var lastErr error
	for {
		observed, lastErr = s.verifyPublicRequest(ctx, nodeID)
		if lastErr == nil {
			return observed, nil
		}
		var certificateError *tls.CertificateVerificationError
		if errors.As(lastErr, &certificateError) {
			return observed, lastErr
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return observed, lastErr
		case <-timer.C:
		}
	}
}

func (s *publicIngressSwitcher) rollback(ctx context.Context, state publicIngressState, code string, cause error) (publicIngressState, error) {
	if state.Trigger == "automatic_health" {
		old, lookupErr := s.h.store.GetProxyNode(ctx, state.ActiveNodeID)
		if lookupErr != nil || old == nil || !eligiblePublicIngressNode(*old) || !s.runtimeIngressHealthy(ctx, *old) {
			state.Phase, state.Error = "failover_pending", code
			state.CompletedAt = s.now().Unix()
			if saveErr := s.save(ctx, state); saveErr != nil {
				return state, saveErr
			}
			if cause == nil {
				cause = errors.New(code)
			}
			return state, cause
		}
	}
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
		if readErr == nil && state.ActiveNodeID == "" {
			readErr = errors.New("rollback_original_node_unknown")
		}
		if readErr == nil {
			state.ObservedNodeID, readErr = s.verifyPublicRequest(ctx, state.ActiveNodeID)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.status(ctx)
	if publicIngressInFlight(state.Phase) {
		return s.recoverAutomaticHealthLocked(ctx, state)
	}
	if state.Phase == "failed" && state.PriorVerifiedID != "" && safePreDNSFailure(state.Error) {
		verified, err := s.verifiedBeforeFailure(ctx, state)
		if err != nil {
			return err
		}
		state = verified
		if err := s.h.store.KV().Put(ctx, publicIngressStateKey, verified); err != nil {
			return err
		}
	}
	// Old UI manual switches wrote fixed by default. Upgrade verified manual
	// operations without changing explicitly pinned API requests.
	if state.Phase == "verified" && state.Mode == "fixed" && state.Trigger == "admin_manual" && state.RequestVerified {
		state.Mode = "preferred"
		if err := s.save(ctx, state); err != nil {
			return err
		}
	}
	if state.Mode == "fixed" {
		if s.schedulerStatus(ctx).Error != "" {
			_ = s.saveSchedulerStatus(ctx, "", "")
		}
		return nil
	}
	if state.Phase != "verified" && state.Phase != "rolled_back" {
		return errors.New("public_ingress_not_initialized_or_verified")
	}
	// Automatic switching is delayed after a verified operation; manual requests are not.
	nodes, err := s.h.store.ListProxyNodes(ctx)
	if err != nil {
		return fmt.Errorf("candidate_list_failed: %w", err)
	}
	var active *storage.ProxyNode
	for i := range nodes {
		if nodes[i].ID == state.ActiveNodeID {
			active = &nodes[i]
			break
		}
	}
	needsSwitch := active == nil || !eligiblePublicIngressNode(*active) || !s.runtimeIngressHealthy(ctx, *active)
	trigger := "automatic_health"
	if !needsSwitch && (active.QuotaBytes <= 0 || active.ThresholdPercent <= 0 || float64(active.UsedBytes)*100 < float64(active.QuotaBytes)*active.ThresholdPercent) && s.schedulerStatus(ctx).Error != "" {
		_ = s.saveSchedulerStatus(ctx, "", "")
	}
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
		_, err = s.switchToLocked(ctx, candidate.ID, trigger, "preferred")
		if err == nil {
			return nil
		}
		failed := s.status(ctx)
		if failed.Phase != "failed" || !safePreDNSFailure(failed.Error) || failed.PriorVerifiedID == "" {
			return err
		}
		verified, checkErr := s.verifiedBeforeFailure(ctx, failed)
		if checkErr != nil {
			return errors.Join(err, checkErr)
		}
		if putErr := s.h.store.KV().Put(ctx, publicIngressStateKey, verified); putErr != nil {
			return errors.Join(err, putErr)
		}
		state = verified
	}
	if err := s.saveSchedulerStatus(ctx, "no_eligible_public_ingress_candidate", trigger); err != nil {
		return err
	}
	return errors.New("no_eligible_public_ingress_candidate")
}

func (s *publicIngressSwitcher) reconcileWithWarning(ctx context.Context) error {
	status := s.schedulerStatus(ctx)
	status.LastAttemptAt, status.NextRunAt = s.now().Unix(), 0
	if err := s.h.store.KV().Put(ctx, publicIngressSchedulerKey, status); err != nil {
		return err
	}
	err := s.reconcile(ctx)
	status = s.schedulerStatus(ctx)
	status.NextRunAt = s.now().Add(30 * time.Second).Unix()
	if err == nil {
		status.LastSuccessAt = s.now().Unix()
	}
	if saveErr := s.h.store.KV().Put(ctx, publicIngressSchedulerKey, status); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	if err == nil || err.Error() == "no_eligible_public_ingress_candidate" {
		return err
	}
	code := "switch_failed"
	switch {
	case err.Error() == "public_ingress_recovery_required":
		code = "recovery_required"
	case err.Error() == "public_ingress_not_initialized_or_verified":
		code = "state_unavailable"
	case strings.HasPrefix(err.Error(), "candidate_list_failed:"):
		code = "candidate_list_failed"
	}
	if s.schedulerStatus(ctx).Error == code {
		return err
	}
	if saveErr := s.saveSchedulerStatus(ctx, code, "automatic"); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func (h *Handler) StartPublicIngressScheduler(ctx context.Context) {
	if h == nil || h.publicIngress == nil {
		return
	}
	h.StartNodeBusinessMonitor(ctx)
	h.StartIngressWatchdog(ctx)
	go func() {
		_ = h.publicIngress.reconcileWithWarning(ctx)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				previousCode := h.publicIngress.schedulerStatus(ctx).Error
				if err := h.publicIngress.reconcileWithWarning(ctx); err != nil && h.log != nil && h.publicIngress.schedulerStatus(ctx).Error != previousCode {
					h.log.Warn("public-ingress", "automatic ingress evaluation failed", map[string]any{"event": "publicIngressReconcileFailed", "code": h.publicIngress.schedulerStatus(ctx).Error})
				}
			}
		}
	}()
}

// SetDNSAutomationClient is intentionally narrow and is used by isolated
// integration tests; production initializes the same client from encrypted KV.
func (h *Handler) SetDNSAutomationClient(client *spaceship.Client) {
	h.dnsAutomation = client
	h.ingressReadback = nil
	if client != nil {
		h.publicIngress = newPublicIngressSwitcher(h)
		h.ingressReadback = &ingressReadback{provider: client, record: h.cfg.PublicIngressHost, lookup: net.DefaultResolver.LookupHost, client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	} else {
		h.publicIngress = nil
	}
}
