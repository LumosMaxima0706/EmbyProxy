package admin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"embyproxy/internal/proxyadapter"
	"embyproxy/internal/validators"
)

// filePlaybackCredentialStore keeps Emby playback tokens outside the SQLite
// node record. Files are root/sidecar-owned, mode 0600, and are never returned
// by an admin API. The directory must be explicitly configured.
type filePlaybackCredentialStore struct{ dir string }

func newFilePlaybackCredentialStore(dir string) (*filePlaybackCredentialStore, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("playback_credential_dir_invalid")
	}
	return &filePlaybackCredentialStore{dir: dir}, nil
}

func (s *filePlaybackCredentialStore) path(slug string) (string, error) {
	slug = validators.NormalizeName(slug)
	if err := proxyadapter.ValidateManagedRouteSlug(slug); err != nil {
		return "", errors.New("playback_credential_slug_invalid")
	}
	return filepath.Join(s.dir, slug+".token"), nil
}

func (s *filePlaybackCredentialStore) PlaybackCredentialConfigured(_ context.Context, slug string) bool {
	p, err := s.path(slug)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.Mode().Perm() == 0600 && st.Size() > 0
}

func (s *filePlaybackCredentialStore) ReadPlaybackCredential(_ context.Context, slug string) (string, error) {
	p, err := s.path(slug)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0600 || st.Size() > 4096 {
		return "", errors.New("playback_credential_missing")
	}
	b, err := os.ReadFile(p)
	if err != nil || strings.ContainsAny(string(b), "\r\n") {
		return "", errors.New("playback_credential_invalid")
	}
	value := strings.TrimSpace(string(b))
	if value == "" {
		return "", errors.New("playback_credential_missing")
	}
	return value, nil
}

func (s *filePlaybackCredentialStore) WritePlaybackCredential(_ context.Context, slug, value string) error {
	p, err := s.path(slug)
	if err != nil || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") || len(value) > 4096 {
		return errors.New("playback_credential_invalid")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(value)), 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *filePlaybackCredentialStore) DeletePlaybackCredential(_ context.Context, slug string) error {
	p, err := s.path(slug)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
