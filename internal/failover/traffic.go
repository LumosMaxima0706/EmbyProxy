package failover

import (
	"context"
	"sync"
	"time"
)

type TrafficSource interface {
	Sample(ctx context.Context, node Node) (TrafficSample, error)
}

type TrafficSourceKind string

const (
	TrafficSourceMock        TrafficSourceKind = "mock"
	TrafficSourceProxyCount  TrafficSourceKind = "proxy_counter"
	TrafficSourceManual      TrafficSourceKind = "manual"
	TrafficSourceProviderAPI TrafficSourceKind = "provider_api"
	TrafficSourceSSHVnstat   TrafficSourceKind = "ssh_vnstat"
)

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
	if inbound < 0 {
		inbound = 0
	}
	if outbound < 0 {
		outbound = 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.byNode[nodeID]
	if s.CycleKey != "" && cycle != "" && s.CycleKey != cycle {
		s = TrafficSample{}
	}
	s.NodeID, s.CycleKey = nodeID, cycle
	s.InboundBytes += inbound
	s.OutboundBytes += outbound
	s.TotalBytes = s.InboundBytes + s.OutboundBytes
	s.Quality = TrafficKnown
	s.SampledAt = time.Now()
	p.byNode[nodeID] = s
}

func (p *ProxyCounter) Reset(nodeID, cycle string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byNode[nodeID] = TrafficSample{NodeID: nodeID, CycleKey: cycle, Quality: TrafficKnown, SampledAt: time.Now()}
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

// ProviderAPISource is a no-credential placeholder. A future implementation
// must load credentials outside this package and must never log them.
type ProviderAPISource struct{}

// SSHVnstatSource is a no-SSH placeholder. It performs no network access.
type SSHVnstatSource struct{}

// ManualTrafficSource is reserved for persisted administrator samples.
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
