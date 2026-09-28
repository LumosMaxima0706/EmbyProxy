package spaceship

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamACMERejectsProviderOverwrite(t *testing.T) {
	value := strings.Repeat("a", 43)
	items := []Record{{Name: "stream", Type: "A", Address: "1.2.3.4"}, {Name: "_acme-challenge.stream", Type: "TXT", Value: "other"}}
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
			return
		}
		if r.Method == http.MethodPut {
			items = []Record{items[0], {Name: "_acme-challenge.stream", Type: "TXT", Value: value}}
			puts++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if err := c.PresentStreamACME(context.Background(), "stream.example.com", value); err == nil {
		t.Fatal("provider overwrote unrelated TXT undetected")
	}
	if puts != 0 || items[1].Value != "other" {
		t.Fatalf("active challenge overwritten: puts=%d records=%+v", puts, items)
	}
}
