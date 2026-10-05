package admin

import (
	"context"
	"time"
)

type ingressWatchdogStatus struct {
	CheckedAt    int64  `json:"checked_at"`
	Error        string `json:"error,omitempty"`
	Notification string `json:"notification"`
}

// Independent goroutine detects a scheduler which stops progressing. It never
// changes DNS, and only uses an already-enabled notification channel.
func (h *Handler) StartIngressWatchdog(ctx context.Context) {
	if h == nil || h.publicIngress == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		lastNotice := ""
		lastSent := int64(0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().Unix()
				st := h.publicIngress.schedulerStatus(ctx)
				code := st.Error
				if st.LastAttemptAt > 0 && now-st.LastAttemptAt > 420 {
					code = "scheduler_stalled"
				}
				operation := h.publicIngress.status(ctx)
				if code == "" && operation.Phase == "failover_pending" && now-operation.StartedAt > 420 {
					code = "failover_pending_too_long"
				}
				if code == "" && operation.ActiveNodeID != "" {
					var probe nodeProbeMonitor
					found, _ := h.store.KV().GetJSON(ctx, "node-probe-monitor:"+operation.ActiveNodeID, &probe)
					if found && now-probe.LastAttemptAt > 300 {
						code = "business_probe_stalled"
					}
				}
				view := ingressWatchdogStatus{CheckedAt: now, Error: code, Notification: "not_required"}
				if code != "" {
					view.Notification = "channel_unconfigured"
					cfg, err := h.store.GetTGConfig(ctx)
					if err == nil && cfg.Enabled && cfg.Token != "" && cfg.Chat != "" && h.telegram != nil {
						view.Notification = "rate_limited"
						if lastNotice != code || now-lastSent >= 900 {
							alertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
							if h.telegram.Send(alertCtx, cfg, "EmbyProxy 自动接管告警\n状态："+code+"\n入口："+operation.ActiveNodeID+"\n请检查反代节点调度状态。") {
								view.Notification = "sent"
							} else {
								view.Notification = "send_failed"
							}
							cancel()
							lastNotice = code
							lastSent = now
						}
					}
					if h.log != nil && (lastNotice != code || now-lastSent >= 900) {
						h.log.Warn("public-ingress", "ingress watchdog requires attention", map[string]any{"event": "publicIngressWatchdog", "code": code, "notification": view.Notification})
						lastNotice = code
						lastSent = now
					}
				} else {
					lastNotice = ""
				}
				_ = h.store.KV().Put(ctx, "failover:public-ingress:watchdog", view)
			}
		}
	}()
}
