package failover

import (
	"context"
	"sync"
	"time"
)

type TrafficSource interface {
	Sample(ctx context.Context, node Node) (TrafficSample, error)
}

type MockTrafficSource struct {
	mu      sync.RWMutex
	samples map[string]TrafficSample
}

func NewMockTrafficSource() *MockTrafficSource {
	return &MockTrafficSource{samples: make(map[string]TrafficSample)}
}

func (m *MockTrafficSource) Set(sample TrafficSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples[sample.NodeID] = sample
}

func (m *MockTrafficSource) Sample(_ context.Context, node Node) (TrafficSample, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sample, ok := m.samples[node.ID]
	if !ok {
		return UnknownTraffic(node.ID), nil
	}
	return sample, nil
}

type ProxyCounter struct {
	mu     sync.Mutex
	byNode map[string]TrafficSample
}

func NewProxyCounter() *ProxyCounter {
	return &ProxyCounter{byNode: make(map[string]TrafficSample)}
}

func (p *ProxyCounter) Add(nodeID string, inbound, outbound int64, cycle string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.byNode[nodeID]
	s.NodeID, s.CycleKey = nodeID, cycle
	s.InboundBytes += inbound
	s.OutboundBytes += outbound
	s.TotalBytes = s.InboundBytes + s.OutboundBytes
	s.Quality = TrafficKnown
	s.SampledAt = time.Now()
	p.byNode[nodeID] = s
}

func (p *ProxyCounter) Sample(_ context.Context, node Node) (TrafficSample, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.byNode[node.ID]
	if !ok {
		return UnknownTraffic(node.ID), nil
	}
	return s, nil
}

// UnknownTraffic is deliberately not a zero-valued sample.
func UnknownTraffic(nodeID string) TrafficSample {
	return TrafficSample{NodeID: nodeID, Quality: TrafficUnknown}
}

// Named placeholders document future adapters without accepting credentials.
type ProviderAPISource struct{}
type SSHVnstatSource struct{}
type ManualTrafficSource struct{}

func (ProviderAPISource) Sample(_ context.Context, node Node) (TrafficSample, error) {
	return UnknownTraffic(node.ID), nil
}
func (SSHVnstatSource) Sample(_ context.Context, node Node) (TrafficSample, error) {
	return UnknownTraffic(node.ID), nil
}
func (ManualTrafficSource) Sample(_ context.Context, node Node) (TrafficSample, error) {
	return UnknownTraffic(node.ID), nil
}
