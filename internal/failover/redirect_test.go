package failover

import "testing"

func TestRedirectRequiresAllowlistedHost(t *testing.T) {
	node := Node{ID: "nosla", PublicHost: "nosla.example"}
	if _, err := BuildRedirect(node, map[string]bool{"other.example": true}, "/s/a/", "x=1"); err != ErrRedirectTargetNotAllowed {
		t.Fatalf("err = %v", err)
	}
	got, err := BuildRedirect(node, map[string]bool{"nosla.example": true}, "/s/a/", "x=1")
	if err != nil || got != "https://nosla.example/s/a/?x=1" {
		t.Fatalf("redirect = %q err=%v", got, err)
	}
}

func TestRedirectRejectsMalformedAllowlistedHost(t *testing.T) {
	node := Node{ID: "nosla", PublicHost: "user@nosla.example"}
	if _, err := BuildRedirect(node, map[string]bool{node.PublicHost: true}, "/media", "x=1"); err != ErrRedirectTargetNotAllowed {
		t.Fatalf("err = %v", err)
	}
}

func TestReasonRedaction(t *testing.T) {
	for _, value := range []string{"Authorization=secret", "path?token=secret", "session-cookie", "https://example.invalid/path", "123e4567-e89b-12d3-a456-426614174000"} {
		if got := RedactReason(value); got != "redacted" {
			t.Fatalf("RedactReason(%q) = %q", value, got)
		}
	}
}
