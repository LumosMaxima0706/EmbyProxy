package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"time"
)

func safePreDNSFailure(code string) bool {
	switch code {
	case "unknown_node", "target_not_eligible", "target_address_invalid", "target_preflight_failed":
		return true
	}
	return false
}
func (s *publicIngressSwitcher) verifiedBeforeFailure(ctx context.Context, failed publicIngressState) (publicIngressState, error) {
	var previous publicIngressState
	raw, found, err := s.h.store.KV().Get(ctx, publicIngressHistoryPrefix+failed.PriorVerifiedID)
	if err != nil || !found || json.Unmarshal([]byte(raw), &previous) != nil || previous.OperationID != failed.PriorVerifiedID || previous.Phase != "verified" || !previous.RequestVerified || previous.ActiveNodeID == "" || previous.ActiveNodeID != failed.ActiveNodeID || previous.RecordName != s.record || previous.DesiredAddress == "" || previous.Mode != failed.Mode {
		return previous, errors.New("verified_ingress_history_unavailable")
	}
	if err := s.rejectIPv6(ctx); err != nil {
		return previous, err
	}
	record, err := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
	if err != nil || record.Address != previous.DesiredAddress {
		return previous, errors.New("verified_ingress_provider_mismatch")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := s.lookupHost(lookupCtx, s.record)
	if err != nil || len(ips) == 0 {
		return previous, errors.New("verified_ingress_recursive_mismatch")
	}
	for _, text := range ips {
		ip, parseErr := netip.ParseAddr(text)
		if parseErr != nil || !ip.Is4() || ip.String() != previous.DesiredAddress {
			return previous, errors.New("verified_ingress_recursive_mismatch")
		}
	}
	if failed.Trigger == "automatic_health" {
		old, lookupErr := s.h.store.GetProxyNode(ctx, previous.ActiveNodeID)
		if lookupErr != nil {
			return previous, lookupErr
		}
		if old == nil || !eligiblePublicIngressNode(*old) || !s.runtimeIngressHealthy(ctx, *old) {
			return previous, nil
		}
	}
	if _, err := s.verifyPublicRequest(lookupCtx, previous.ActiveNodeID); err != nil {
		return previous, errors.New("verified_ingress_public_mismatch")
	}
	return previous, nil
}
