package admin

import (
	"strings"
	"testing"
)

func TestManagedRoutesUIContract(t *testing.T) {
	for _, marker := range []string{
		"switchTab('managedRoutes',this)",
		"id=\"tabManagedRoutes\"",
		"/api/admin/managed-routes",
		"function loadManagedRoutes(",
		"function saveManagedRoute(",
		"function deleteManagedRoute(",
		"function addManagedRouteLine(",
		"credentials: 'same-origin'",
	} {
		if !strings.Contains(indexHTML, marker) {
			t.Fatalf("managed route UI marker %q is missing", marker)
		}
	}
	for _, forbidden := range []string{
		"strong-admin-token",
		"Authorization: Bearer",
		"Set-Cookie",
	} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("managed route UI embeds sensitive marker %q", forbidden)
		}
	}
}
