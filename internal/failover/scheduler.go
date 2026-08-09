package failover

import (
	"context"
	"errors"
	"time"
)

type Scheduler struct {
	Controller *Controller
	Interval   time.Duration
	OnDecision func(Decision) error
	OnError    func(error)
	Probe      HealthProbe
	Traffic    TrafficSource
}

func (s Scheduler) RunOnce() (Decision, error) {
	return s.RunOnceContext(context.Background())
}

func (s Scheduler) RunOnceContext(ctx context.Context) (Decision, error) {
	if s.Controller == nil {
		return s.fail(Decision{Reason: "controller_unavailable"}, errors.New("controller unavailable"))
	}
	_, nodes := s.Controller.Status()
	for _, node := range nodes {
		if s.Probe != nil {
			if err := s.Controller.SetHealth(s.Probe.Check(ctx, node)); err != nil {
				return s.fail(Decision{Reason: "health_update_failed"}, err)
			}
		}
		if s.Traffic != nil {
			sample, err := s.Traffic.Sample(ctx, node)
			if err != nil {
				sample = UnknownTraffic(node.ID)
			}
			if err := s.Controller.SetTraffic(sample); err != nil {
				return s.fail(Decision{Reason: "traffic_update_failed"}, err)
			}
		}
	}
	decision, err := s.Controller.Evaluate()
	if err != nil {
		return s.fail(decision, err)
	}
	if s.OnDecision != nil {
		if err := s.OnDecision(decision); err != nil {
			return s.fail(decision, err)
		}
	}
	return decision, nil
}

func (s Scheduler) fail(decision Decision, err error) (Decision, error) {
	if s.OnError != nil {
		s.OnError(err)
	}
	return decision, err
}

func (s Scheduler) Run(ctx context.Context) error {
	if s.Controller == nil {
		_, err := s.fail(Decision{Reason: "controller_unavailable"}, errors.New("controller unavailable"))
		return err
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
			return ctx.Err()
		case <-ticker.C:
			// The scheduler only emits a decision. Applying it remains an
			// explicit caller action in this local-only phase.
			if _, err := s.RunOnceContext(ctx); err != nil {
				return err
			}
		}
	}
}
