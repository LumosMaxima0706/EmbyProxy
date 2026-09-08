package edgecontrol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DecommissionType = "DECOMMISSION"

var (
	ErrInvalidSignature = errors.New("invalid controller signature")
	ErrWrongNode        = errors.New("decommission node mismatch")
	ErrExpired          = errors.New("decommission job expired")
	ErrReplay           = errors.New("decommission job replayed")
	ErrInvalidType      = errors.New("unsupported control job type")
	ErrNonceStore       = errors.New("decommission nonce store unavailable")
)

// Job is deliberately a closed protocol. There is no command or shell field.
// The edge is only allowed to execute the fixed DECOMMISSION operation.
type Job struct {
	Type            string `json:"type"`
	JobID           string `json:"job_id"`
	NodeID          string `json:"node_id"`
	Nonce           string `json:"nonce"`
	CreatedAt       int64  `json:"created_at"`
	ExpiresAt       int64  `json:"expires_at"`
	CompletionToken string `json:"completion_token,omitempty"`
	Signature       string `json:"signature"`
}

type unsignedJob struct {
	Type            string `json:"type"`
	JobID           string `json:"job_id"`
	NodeID          string `json:"node_id"`
	Nonce           string `json:"nonce"`
	CreatedAt       int64  `json:"created_at"`
	ExpiresAt       int64  `json:"expires_at"`
	CompletionToken string `json:"completion_token,omitempty"`
}

func canonical(j Job) ([]byte, error) {
	return json.Marshal(unsignedJob{j.Type, j.JobID, j.NodeID, j.Nonce, j.CreatedAt, j.ExpiresAt, j.CompletionToken})
}

func NewKey() (ed25519.PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	return key, err
}

func EncodePublicKey(key ed25519.PublicKey) string { return base64.RawStdEncoding.EncodeToString(key) }

func DecodePublicKey(value string) (ed25519.PublicKey, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid controller public key")
	}
	return ed25519.PublicKey(raw), nil
}

func NewJob(nodeID, jobID, completionToken string, now time.Time, ttl time.Duration, key ed25519.PrivateKey) (Job, error) {
	if len(key) != ed25519.PrivateKeySize || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(jobID) == "" {
		return Job{}, errors.New("invalid decommission job input")
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return Job{}, err
	}
	j := Job{Type: DecommissionType, JobID: jobID, NodeID: nodeID, Nonce: base64.RawURLEncoding.EncodeToString(buf), CreatedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(), CompletionToken: completionToken}
	if err := Sign(&j, key); err != nil {
		return Job{}, err
	}
	return j, nil
}

func Sign(j *Job, key ed25519.PrivateKey) error {
	if j == nil || len(key) != ed25519.PrivateKeySize {
		return errors.New("invalid signing key")
	}
	raw, err := canonical(*j)
	if err != nil {
		return err
	}
	j.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, raw))
	return nil
}

type NonceStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewNonceStore() *NonceStore { return &NonceStore{seen: make(map[string]time.Time)} }

func (s *NonceStore) Verify(j Job, nodeID string, key ed25519.PublicKey, now time.Time, maxTTL time.Duration) error {
	if j.Type != DecommissionType {
		return ErrInvalidType
	}
	if j.NodeID != nodeID {
		return ErrWrongNode
	}
	if j.ExpiresAt <= now.Unix() || j.CreatedAt > now.Add(2*time.Minute).Unix() || j.ExpiresAt-j.CreatedAt > int64(maxTTL/time.Second) {
		return ErrExpired
	}
	sig, err := base64.RawStdEncoding.DecodeString(j.Signature)
	if err != nil {
		return ErrInvalidSignature
	}
	raw, err := canonical(j)
	if err != nil || !ed25519.Verify(key, raw, sig) {
		return ErrInvalidSignature
	}
	if strings.TrimSpace(j.Nonce) == "" || strings.TrimSpace(j.JobID) == "" {
		return ErrReplay
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for nonce, expiry := range s.seen {
		if expiry.Before(now) {
			delete(s.seen, nonce)
		}
	}
	if _, ok := s.seen[j.Nonce]; ok {
		return ErrReplay
	}
	s.seen[j.Nonce] = time.Unix(j.ExpiresAt, 0)
	return nil
}

// PersistentNonceStore lets an edge survive process restart and reboot without
// accepting a previously consumed control job. The file contains only nonce
// hashes and expiry timestamps, never the signed payload or completion token.
type PersistentNonceStore struct {
	Path string
	mu   sync.Mutex
}

func (s *PersistentNonceStore) Verify(j Job, nodeID string, key ed25519.PublicKey, now time.Time, maxTTL time.Duration) error {
	if strings.TrimSpace(s.Path) == "" {
		return ErrNonceStore
	}
	// Authenticate the immutable payload before consulting or mutating replay
	// state. Otherwise an attacker who can submit an invalid signature could
	// poison a valid nonce entry.
	if err := verifyFieldsAndSignature(j, nodeID, key, now, maxTTL); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrNonceStore
	}
	seen := map[string]int64{}
	if len(data) > 0 && json.Unmarshal(data, &seen) != nil {
		return ErrNonceStore
	}
	for nonce, expiry := range seen {
		if expiry <= now.Unix() {
			delete(seen, nonce)
		}
	}
	keyNonce := nonceHash(nodeID + "\x00" + j.Nonce)
	if expiry, ok := seen[keyNonce]; ok && expiry > now.Unix() {
		return ErrReplay
	}
	seen[keyNonce] = j.ExpiresAt
	raw, err := json.Marshal(seen)
	if err != nil {
		return ErrNonceStore
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return ErrNonceStore
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".decommission-nonces-*")
	if err != nil {
		return ErrNonceStore
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return ErrNonceStore
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return ErrNonceStore
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return ErrNonceStore
	}
	if err := tmp.Close(); err != nil {
		return ErrNonceStore
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return ErrNonceStore
	}
	return nil
}

func nonceHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func verifyFieldsAndSignature(j Job, nodeID string, key ed25519.PublicKey, now time.Time, maxTTL time.Duration) error {
	if j.Type != DecommissionType {
		return ErrInvalidType
	}
	if j.NodeID != nodeID {
		return ErrWrongNode
	}
	if j.ExpiresAt <= now.Unix() || j.CreatedAt > now.Add(2*time.Minute).Unix() || j.ExpiresAt-j.CreatedAt > int64(maxTTL/time.Second) {
		return ErrExpired
	}
	sig, err := base64.RawStdEncoding.DecodeString(j.Signature)
	if err != nil {
		return ErrInvalidSignature
	}
	raw, err := canonical(j)
	if err != nil || !ed25519.Verify(key, raw, sig) {
		return ErrInvalidSignature
	}
	if strings.TrimSpace(j.Nonce) == "" || strings.TrimSpace(j.JobID) == "" {
		return ErrReplay
	}
	return nil
}

func CompletionTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}
