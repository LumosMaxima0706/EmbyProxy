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
