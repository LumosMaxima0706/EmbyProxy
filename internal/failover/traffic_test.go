package failover

import (
	"context"
	"testing"
)

func TestMockTrafficMissingIsUnknown(t *testing.T) {
	source := NewMockTrafficSource()
	sample, err := source.Sample(context.Background(), Node{ID: "nosla"})
	if err != nil || sample.Quality != TrafficUnknown || sample.NodeID != "nosla" {
		t.Fatalf("sample = %+v err=%v", sample, err)
	}
}

func TestProxyCounterCountsLocalTraffic(t *testing.T) {
	counter := NewProxyCounter()
	counter.Add("bwg", 100, 250, "cycle-a")
	counter.Add("bwg", 50, 75, "cycle-a")
	sample, err := counter.Sample(context.Background(), Node{ID: "bwg"})
	if err != nil || sample.InboundBytes != 150 || sample.OutboundBytes != 325 || sample.TotalBytes != 475 || sample.Quality != TrafficKnown {
		t.Fatalf("sample = %+v err=%v", sample, err)
	}
}

func TestProxyCounterStartsNewBillingCycle(t *testing.T) {
	counter := NewProxyCounter()
	counter.Add("nosla", 100, 200, "cycle-a")
	counter.Add("nosla", 10, 20, "cycle-b")
	sample, err := counter.Sample(context.Background(), Node{ID: "nosla"})
	if err != nil || sample.CycleKey != "cycle-b" || sample.TotalBytes != 30 {
		t.Fatalf("sample = %+v err=%v", sample, err)
	}
}

func TestProxyCounterRejectsNegativeBytes(t *testing.T) {
	counter := NewProxyCounter()
	counter.Add("bwg", -1, -2, "cycle-a")
	sample, _ := counter.Sample(context.Background(), Node{ID: "bwg"})
	if sample.TotalBytes != 0 {
		t.Fatalf("sample = %+v", sample)
	}
}

func TestControllerRejectsInvalidKnownTraffic(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	if err := c.SetTraffic(TrafficSample{NodeID: "nosla", Quality: TrafficKnown, InboundBytes: -1}); err != ErrInvalidTraffic {
		t.Fatalf("err = %v", err)
	}
}
