package admin

import (
	"context"
	"errors"
	"time"
)

// confirmPreviousIngress is read-only: it cannot restore DNS, only verify
// that the original address and original node already serve new connections.
func (s *publicIngressSwitcher) confirmPreviousIngress(ctx context.Context, operationID string) (publicIngressState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	state := s.status(ctx)
	if operationID == "" || state.OperationID != operationID || !publicIngressInFlight(state.Phase) || state.PreviousAddress == "" || state.ActiveNodeID == "" {
		return state, errors.New("recovery_state_mismatch")
	}
	if err := s.rejectIPv6(ctx); err != nil {
		return state, err
	}
	record, err := s.h.dnsAutomation.ExactRecord(ctx, s.record, "A")
	if err != nil {
		return state, err
	}
	if record.Address != state.PreviousAddress {
		return state, errors.New("recovery_provider_not_original")
	}
	state.ProviderAddress = record.Address
	state.RecursiveAddress, err = s.waitRecursive(ctx, state.PreviousAddress, 90*time.Second)
	if err != nil {
		return state, err
	}
	state.ObservedNodeID, err = s.verifyPublicRequest(ctx, state.ActiveNodeID)
	if err != nil {
		return state, err
	}
	state.RollbackAttempted = true
	state.RollbackVerified = true
	state.RequestVerified = true
	state.Phase = "rolled_back"
	state.CompletedAt = s.now().Unix()
	state.Error = ""
	if err := s.save(ctx, state); err != nil {
		return state, err
	}
	return state, nil
}
