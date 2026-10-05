package admin

import (
	"context"
	"errors"
	"time"
)

// Resume only a known automatic health transaction, never an unknown/manual
// transaction or a DNS address written by another actor. A dead predecessor is
// not a rollback destination. The mutex is held by reconcile.
func (s *publicIngressSwitcher) recoverAutomaticHealthLocked(ctx context.Context, state publicIngressState) error {
	if state.Trigger != "automatic_health" || state.Mode != "preferred" || state.PreviousAddress == "" || state.DesiredAddress == "" || state.RecordName != s.record {
		return errors.New("public_ingress_recovery_required")
	}
	old, err := s.h.store.GetProxyNode(ctx, state.ActiveNodeID)
	if err != nil {
		return err
	}
	if old != nil && eligiblePublicIngressNode(*old) && s.runtimeIngressHealthy(ctx, *old) {
		return errors.New("public_ingress_recovery_required")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 6*time.Minute)
	defer cancel()
	if err := s.rejectIPv6(ctx); err != nil {
		return err
	}
	record, err := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
	if err != nil {
		return err
	}
	if record.Address != state.PreviousAddress && record.Address != state.DesiredAddress && (state.RecoverySourceAddress == "" || record.Address != state.RecoverySourceAddress) {
		return errors.New("automatic_recovery_provider_changed_concurrently")
	}
	nodes, err := s.h.store.ListProxyNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.ID == state.ActiveNodeID || !eligiblePublicIngressNode(node) || (node.QuotaBytes > 0 && node.ThresholdPercent > 0 && float64(node.UsedBytes)*100 >= float64(node.QuotaBytes)*node.ThresholdPercent) {
			continue
		}
		ip, e := s.proxyNodeIPv4(ctx, node)
		if e != nil {
			continue
		}
		if e = s.preflight(ctx, node, ip); e != nil {
			continue
		}
		if e = s.runtimeMediaPreflight(ctx, node); e != nil {
			continue
		}
		// Persist intent before CAS; interrupted operations can resume on restart.
		state.RecoverySourceAddress = record.Address
		state.RequestedNodeID, state.DesiredAddress = node.ID, ip.String()
		state.Phase, state.Error, state.RequestVerified = "failover_pending", "", false
		if e = s.save(ctx, state); e != nil {
			return e
		}
		if record.Address != state.DesiredAddress {
			updated, updateErr := s.h.dnsAutomation.ReplaceExactA(ctx, s.record, record.Address, state.DesiredAddress, s.ttl)
			if updateErr != nil {
				state.Error = "provider_update_unverified"
				_ = s.save(ctx, state)
				return updateErr
			}
			record = updated
		}
		state.ProviderAddress = record.Address
		if e = s.save(ctx, state); e != nil {
			return e
		}
		state.RecursiveAddress, e = s.waitRecursive(ctx, state.DesiredAddress, 4*time.Minute)
		if e != nil {
			state.Error = "recursive_verification_failed"
			_ = s.save(ctx, state)
			return e
		}
		state.ObservedNodeID, e = s.waitPublicRequest(ctx, node.ID, 30*time.Second)
		if e != nil {
			state.Error = "public_request_failed"
			state.RequestError = publicRequestErrorCode(e)
			_ = s.save(ctx, state)
			return e
		}
		state.ActiveNodeID, state.Mode, state.Phase = node.ID, "preferred", "verified"
		state.RequestVerified, state.Error, state.CompletedAt = true, "", s.now().Unix()
		if e = s.save(ctx, state); e != nil {
			return e
		}
		return s.saveSchedulerStatus(ctx, "", "automatic_health")
	}
	state.Phase, state.Error = "failover_pending", "no_eligible_public_ingress_candidate"
	if err := s.save(ctx, state); err != nil {
		return err
	}
	if err := s.saveSchedulerStatus(ctx, "no_eligible_public_ingress_candidate", "automatic_health"); err != nil {
		return err
	}
	return errors.New("no_eligible_public_ingress_candidate")
}
