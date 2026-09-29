package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecursiveAnswersRequireOnlyTargetIPv4(t *testing.T) {
	for _, tc := range []struct {
		name string
		ips  []string
		want bool
	}{
		{"one", []string{"1.1.1.1"}, true},
		{"duplicates", []string{"1.1.1.1", "1.1.1.1"}, true},
		{"mixed", []string{"1.1.1.1", "2.2.2.2"}, false},
		{"ipv6", []string{"1.1.1.1", "2001:db8::1"}, false},
		{"invalid", []string{"1.1.1.1", "invalid"}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recursiveAnswersMatch(tc.ips, "1.1.1.1"); got != tc.want {
				t.Fatalf("answers=%v matched=%t want=%t", tc.ips, got, tc.want)
			}
		})
	}
}

func TestWaitRecursiveRejectsMixedAnswers(t *testing.T) {
	s := newPublicIngressSwitcher(&Handler{})
	s.record = "stream.example.com"
	s.now = func() time.Time { return time.Now() }
	s.lookupHost = func(context.Context, string) ([]string, error) {
		return []string{"1.1.1.1", "2.2.2.2"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.waitRecursive(ctx, "1.1.1.1", time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("mixed results were accepted: %v", err)
	}
	s.lookupHost = func(context.Context, string) ([]string, error) { return []string{"1.1.1.1"}, nil }
	if got, err := s.waitRecursive(context.Background(), "1.1.1.1", time.Second); err != nil || got != "1.1.1.1" {
		t.Fatalf("matching results rejected: %s %v", got, err)
	}
}
