package edgecontrol

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecommissionJobVerification(t *testing.T) {
	pub, private, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_700_000_000, 0)
	job, err := NewJob("node-a", "job-a", "completion", now, 5*time.Minute, private)
	if err != nil {
		t.Fatal(err)
	}
	store := NewNonceStore()
	if err := store.Verify(job, "node-a", pub, now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(job, "node-a", pub, now, 10*time.Minute); err != ErrReplay {
		t.Fatalf("replay=%v", err)
	}
	job.NodeID = "node-b"
	if err := NewNonceStore().Verify(job, "node-a", pub, now, 10*time.Minute); err != ErrWrongNode {
		t.Fatalf("wrong node=%v", err)
	}
	job.NodeID = "node-a"
	job.ExpiresAt = now.Add(-time.Second).Unix()
	if err := NewNonceStore().Verify(job, "node-a", pub, now, 10*time.Minute); err != ErrExpired {
		t.Fatalf("expired=%v", err)
	}
}

func TestPersistentNonceStoreRejectsReplayAfterRestart(t *testing.T) {
	pub, private, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_700_000_000, 0)
	job, err := NewJob("node-a", "job-a", "completion", now, 5*time.Minute, private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state", "nonces.json")
	if err := (&PersistentNonceStore{Path: path}).Verify(job, "node-a", pub, now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("nonce state permissions: info=%v err=%v", info, err)
	}
	// A distinct store instance represents an edge process restart or host
	// reboot. The persisted nonce must still prevent replay.
	if err := (&PersistentNonceStore{Path: path}).Verify(job, "node-a", pub, now, 10*time.Minute); err != ErrReplay {
		t.Fatalf("restart replay=%v", err)
	}
}

func TestPersistentNonceStoreAuthenticatesBeforeRecordingNonce(t *testing.T) {
	pub, private, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_700_000_000, 0)
	job, err := NewJob("node-a", "job-a", "completion", now, 5*time.Minute, private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nonces.json")
	invalid := job
	invalid.Signature = "invalid"
	if err := (&PersistentNonceStore{Path: path}).Verify(invalid, "node-a", pub, now, 10*time.Minute); err != ErrInvalidSignature {
		t.Fatalf("invalid signature=%v", err)
	}
	if err := (&PersistentNonceStore{Path: path}).Verify(job, "node-a", pub, now, 10*time.Minute); err != nil {
		t.Fatalf("valid job after invalid attempt=%v", err)
	}
}
