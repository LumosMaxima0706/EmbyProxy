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

func TestControllerDryRunRecordsWithoutChangingState(t *testing.T) {
	provider := NewMockDNSProvider()
	var recorded int
	c := NewController(testNodes(), DefaultPolicyConfig(), provider)
	c.SetDNSRunWriter(func(change DNSChange, success bool) error {
		recorded++
		if !change.DryRun || !success {
			t.Fatalf("change=%+v success=%v", change, success)
		}
		return nil
	})
	before, _ := c.Status()
	plan, err := c.DryRunDNS(context.Background(), DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.10", TTL: 60})
	if err != nil || !plan.Change.DryRun || recorded != 1 {
		t.Fatalf("plan=%+v err=%v recorded=%d", plan, err, recorded)
	}
	after, _ := c.Status()
	if after.ActiveNodeID != before.ActiveNodeID {
		t.Fatalf("state changed: before=%+v after=%+v", before, after)
	}
}

func TestDNSRejectsUnsafeRecordInput(t *testing.T) {
	provider := NewMockDNSProvider()
	for _, change := range []DNSChange{
		{Name: "", Type: "A", Value: "192.0.2.1"},
		{Name: "stream.example", Type: "TXT", Value: "x"},
		{Name: "stream.example", Type: "A", Value: ""},
		{Name: "stream.example", Type: "A", Value: "192.0.2.1", TTL: 90000},
	} {
		if _, err := ApplyDNS(context.Background(), provider, change); err == nil {
			t.Fatalf("change %+v unexpectedly accepted", change)
		}
	}
}
