package spaceship

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamACMERecordIsolation(t *testing.T) {
	value := strings.Repeat("b", 43)
	items := []Record{{Name: "stream", Type: "A", Address: "1.2.3.4", TTL: 60}, {Name: "_acme-challenge.stream", Type: "TXT", Value: "other", TTL: 60}}
	puts, deletes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
		case http.MethodPut:
			var body recordsWriteRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Items) != 1 || body.Items[0].Name != "_acme-challenge.stream" || body.Items[0].Type != "TXT" || body.Items[0].Value != value {
				t.Fatalf("unsafe PUT: %+v err=%v", body, err)
			}
			puts++
			items = append(items, body.Items[0])
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			var body []recordDeleteRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 || body[0].Name != "_acme-challenge.stream" || body[0].Type != "TXT" || body[0].Value != value || body[0].Address != "" {
				t.Fatalf("unsafe DELETE: %+v err=%v", body, err)
			}
			deletes++
			items = items[:2]
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	ctx := context.Background()
	if err := c.PresentStreamACME(ctx, "stream.example.com", value); err != nil {
		t.Fatal(err)
	}
	if err := c.PresentStreamACME(ctx, "stream.example.com", value); err != nil {
		t.Fatal(err)
	}
	if puts != 1 || len(items) != 3 {
		t.Fatalf("non-idempotent TXT: puts=%d records=%d", puts, len(items))
	}
	if err := c.CleanupStreamACME(ctx, "stream.example.com", value); err != nil {
		t.Fatal(err)
	}
	if err := c.CleanupStreamACME(ctx, "stream.example.com", value); err != nil {
		t.Fatal(err)
	}
	if puts != 1 || deletes != 1 || len(items) != 2 {
		t.Fatalf("unsafe cleanup: puts=%d deletes=%d items=%+v", puts, deletes, items)
	}
	if items[0].Type != "A" || items[0].Address != "1.2.3.4" || items[1].Value != "other" {
		t.Fatalf("other records changed: %+v", items)
	}
}
