package failover

import (
	"context"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Interval   time.Duration
	OnDecision func(Decision)
	Probe      HealthProbe
	Traffic    TrafficSource
}

func (s Scheduler) RunOnce() Decision {
	return s.RunOnceContext(context.Background())
}

func (s Scheduler) RunOnceContext(ctx context.Context) Decision {
	if s.Controller == nil {
		return Decision{Reason: "controller_unavailable"}
	}
	_, nodes := s.Controller.Status()
	for _, node := range nodes {
		if s.Probe != nil {
			_ = s.Controller.SetHealth(s.Probe.Check(ctx, node))
		}
		if s.Traffic != nil {
			sample, err := s.Traffic.Sample(ctx, node)
			if err != nil {
				sample = UnknownTraffic(node.ID)
			}
			_ = s.Controller.SetTraffic(sample)
		}
	}
	decision := s.Controller.Evaluate()
	if s.OnDecision != nil {
		s.OnDecision(decision)
	}
	return decision
}

func (s Scheduler) Run(ctx context.Context) {
	if s.Controller == nil {
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// The scheduler only emits a decision. Applying it remains an
			// explicit caller action in this local-only phase.
			s.RunOnce()
		}
	}
}
