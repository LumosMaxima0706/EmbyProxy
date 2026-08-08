package failover

import (
	"context"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Interval   time.Duration
	OnDecision func(Decision)
}

func (s Scheduler) RunOnce() Decision {
	if s.Controller == nil {
		return Decision{Reason: "controller_unavailable"}
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
