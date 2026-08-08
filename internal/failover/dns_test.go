package failover

import (
	"context"
	"testing"
)

func TestMockDNSDryRunDoesNotApply(t *testing.T) {
	mock := NewMockDNSProvider()
	change := DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.10", TTL: 60, DryRun: true}
	if _, err := ApplyDNS(context.Background(), mock, change); err != nil {
		t.Fatal(err)
	}
	if mock.ApplyCount != 0 {
		t.Fatalf("ApplyCount = %d", mock.ApplyCount)
	}
}

func TestMockDNSApplyAndVerify(t *testing.T) {
	mock := NewMockDNSProvider()
	change := DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.10", TTL: 60}
	propagation, err := ApplyDNS(context.Background(), mock, change)
	if err != nil || !propagation.Verified {
		t.Fatalf("propagation = %+v err=%v", propagation, err)
	}
}

func TestDNSFailureDoesNotCommitActiveState(t *testing.T) {
	mock := NewMockDNSProvider()
	mock.FailApply = true
	c := NewController(testNodes(), DefaultPolicyConfig(), mock)
	before, _ := c.Status()
	err := c.ApplyDNSAndCommit(context.Background(), DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.11", TTL: 60}, "bwg")
	if err == nil {
		t.Fatal("expected apply failure")
	}
	after, _ := c.Status()
	if after.ActiveNodeID != before.ActiveNodeID || !after.ReconciliationRequired {
		t.Fatalf("state changed unexpectedly: before=%+v after=%+v", before, after)
	}
}

func TestDNSVerifyFailureDoesNotCommitActiveState(t *testing.T) {
	mock := NewMockDNSProvider()
	mock.FailVerify = true
	c := NewController(testNodes(), DefaultPolicyConfig(), mock)
	before, _ := c.Status()
	err := c.ApplyDNSAndCommit(context.Background(), DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.12", TTL: 60}, "bwg")
	if err == nil {
		t.Fatal("expected verification failure")
	}
	after, _ := c.Status()
	if after.ActiveNodeID != before.ActiveNodeID || !after.ReconciliationRequired {
		t.Fatalf("state changed unexpectedly: before=%+v after=%+v", before, after)
	}
}
