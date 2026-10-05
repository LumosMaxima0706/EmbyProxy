package admin

import (
	"context"
	"embyproxy/internal/storage"
	"errors"
)

func (s *publicIngressSwitcher) runtimeMediaPreflight(ctx context.Context, node storage.ProxyNode) error {
	// A missing provider is reported as unverified, not fabricated playback.
	if s.h.playbackCredentials == nil {
		return nil
	}
	view := s.h.checkProxyNodeReadiness(ctx, node)
	if err := s.h.store.KV().Put(context.WithoutCancel(ctx), "node-readiness:"+node.ID, view); err != nil {
		return err
	}
	if view.BusinessTLS != "ready" || view.Routes == "failed" || view.Playback == "failed" {
		return errors.New("runtime_business_or_media_failed")
	}
	return nil
}
