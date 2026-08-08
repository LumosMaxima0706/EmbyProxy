package failover

import (
	"context"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Interval   time.Duration
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
			decision := s.Controller.Evaluate()
			if decision.Change {
				// Phase 2A is mock-only. Callers explicitly apply decisions in tests.
				_ = decision
			}
		}
	}
}
