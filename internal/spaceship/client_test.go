package spaceship

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestStreamACMERejectsConflictingRecordWithoutWrite(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []Record{{Name: "_acme-challenge.stream", Type: "CNAME", Address: "other.example.com"}}, "total": 1})
			return
		}
		writes++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if err := c.PresentStreamACME(context.Background(), "stream.example.com", strings.Repeat("a", 43)); err == nil || !strings.Contains(err.Error(), "record_conflict") {
		t.Fatalf("conflicting record not rejected: %v", err)
	}
	if writes != 0 {
		t.Fatalf("conflict caused provider write: %d", writes)
	}
}
func TestStreamACMERejectsUnmanagedHostAndUnsafeValue(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	for _, host := range []string{"other.example.com", "stream.other.com"} {
		if err := c.PresentStreamACME(context.Background(), host, strings.Repeat("a", 43)); err == nil {
			t.Fatalf("unmanaged host accepted: %s", host)
		}
	}
	if err := c.CleanupStreamACME(context.Background(), "stream.example.com", "unsafe/value"); err == nil {
		t.Fatal("unsafe validation accepted")
	}
	if writes != 0 {
		t.Fatalf("provider contacted: %d", writes)
	}
}

func TestEnsureAIdempotentAndConflictSafe(t *testing.T) {
	putCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "key" || r.Header.Get("X-API-Secret") != "secret" {
			t.Fatal("credentials missing")
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"records":[{"id":"r1","name":"rak","type":"A","address":"1.2.3.4","ttl":300}]}`))
			return
		}
		if r.Method == http.MethodPut {
			putCalls++
			t.Fatal("identical record must not write")
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "key", APISecret: "secret", ManagedDomain: "example.com"}
	r, err := c.EnsureA(context.Background(), "rak", netip.MustParseAddr("1.2.3.4"), 300)
	if err != nil || r.ID != "r1" || putCalls != 0 {
		t.Fatalf("idempotent result=%+v err=%v puts=%d", r, err, putCalls)
	}
}

func TestEnsureARejectsUnmanagedAndConflicting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"records":[{"id":"c1","name":"x","type":"CNAME","address":"other.example","ttl":300}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if _, err := c.EnsureA(context.Background(), "x", netip.MustParseAddr("1.2.3.4"), 300); err == nil {
		t.Fatal("expected conflicting record rejection")
	}
	c.BaseURL = "http://127.0.0.1"
	if _, err := c.EnsureA(context.Background(), "x", netip.MustParseAddr("1.2.3.4"), 300); err == nil {
		t.Fatal("expected transport failure")
	}
	if _, err := c.endpoint("other.test"); err == nil {
		t.Fatal("unmanaged domain accepted")
	}
}

func TestClientPutShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"records":[]}`))
			return
		}
		var body recordsWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Force || len(body.Items) != 1 {
			t.Fatalf("unexpected put body: %+v err=%v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if _, err := c.EnsureA(context.Background(), "new", netip.MustParseAddr("1.2.3.4"), 300); err == nil {
		t.Fatal("expected verification failure from empty readback")
	}
}

func TestListUsesPaginationAndPreservesStatus(t *testing.T) {
	for _, code := range []int{200, 400, 401, 403, 404, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("take") != "500" || r.URL.Query().Get("skip") != "0" {
					t.Errorf("query=%s", r.URL.RawQuery)
				}
				w.WriteHeader(code)
				if code == 200 {
					_, _ = w.Write([]byte(`{"records":[]}`))
				} else {
					_, _ = w.Write([]byte(`{"detail":"safe provider detail"}`))
				}
			}))
			defer srv.Close()
			c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
			status, detail, err := c.TestStatus(context.Background())
			if code == 200 {
				if status != 200 || err != nil {
					t.Fatalf("status=%d err=%v", status, err)
				}
			} else if status != code || detail != "safe provider detail" {
				t.Fatalf("status=%d detail=%q err=%v", status, detail, err)
			}
		})
	}
}

func TestListAcceptsSpaceshipItemsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("take") != "500" || r.URL.Query().Get("skip") != "0" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"name":"rak","type":"A","address":"1.2.3.4","ttl":300}],"total":1}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	records, err := c.List(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("list error=%v", err)
	}
	if len(records) != 1 || records[0].Name != "rak" || records[0].Address != "1.2.3.4" {
		t.Fatalf("records=%+v", records)
	}
	status, detail, err := c.TestStatus(context.Background())
	if status != http.StatusOK || detail != "" || err != nil {
		t.Fatalf("test status=%d detail=%q err=%v", status, detail, err)
	}
}

func TestDeleteExactUsesScopedPayloadAndPreservesUnrelatedRecords(t *testing.T) {
	calls := 0
	remaining := []Record{{Name: "keep", Type: "A", Address: "5.6.7.8", TTL: 300}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			calls++
			items := remaining
			if calls == 1 {
				items = append([]Record{{Name: "remove", Type: "A", Address: "1.2.3.4", TTL: 300}}, remaining...)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
		case http.MethodDelete:
			var body []map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 || len(body[0]) != 3 || body[0]["name"] != "remove" || body[0]["type"] != "A" || body[0]["address"] != "1.2.3.4" {
				t.Fatalf("delete body=%+v err=%v", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if err := c.DeleteExact(context.Background(), "remove.example.com", "A", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteExactRejectsChangedAddressAndInvalidIdentity(t *testing.T) {
	deleteCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalls++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Record{{Name: "remove", Type: "A", Address: "9.9.9.9", TTL: 300}}, "total": 1})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if err := c.DeleteExact(context.Background(), "remove.example.com", "A", "1.2.3.4"); err == nil {
		t.Fatal("changed address accepted")
	}
	if err := c.DeleteExact(context.Background(), "remove.example.com", "A", "not-an-ip"); err == nil {
		t.Fatal("invalid address accepted")
	}
	if err := c.DeleteExact(context.Background(), "outside.test", "A", "1.2.3.4"); err == nil {
		t.Fatal("outside domain accepted")
	}
	if deleteCalls != 0 {
		t.Fatalf("delete calls=%d", deleteCalls)
	}
}

func TestListRejectsIncompleteSpaceshipPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"name":"one","type":"A","address":"1.2.3.4"}],"total":501}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	if _, err := c.List(context.Background(), "example.com"); err == nil {
		t.Fatal("incomplete provider page accepted")
	}
}

func TestReplaceExactACompareAndSwapAndReadback(t *testing.T) {
	current := Record{ID: "r1", Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}
	putCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []Record{current}, "total": 1})
		case http.MethodPut:
			var body recordsWriteRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Items) != 1 {
				t.Fatalf("body=%+v err=%v", body, err)
			}
			putCalls++
			current = body.Items[0]
			current.ID = "r1"
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	got, err := c.ReplaceExactA(context.Background(), "stream.example.com", "1.1.1.1", "2.2.2.2", 60)
	if err != nil || got.Address != "2.2.2.2" || putCalls != 1 {
		t.Fatalf("got=%+v err=%v puts=%d", got, err, putCalls)
	}
	if _, err = c.ReplaceExactA(context.Background(), "stream.example.com", "1.1.1.1", "3.3.3.3", 60); err == nil {
		t.Fatal("stale expected value accepted")
	}
	got, err = c.ReplaceExactA(context.Background(), "stream.example.com", "2.2.2.2", "2.2.2.2", 60)
	if err != nil || got.Address != "2.2.2.2" || putCalls != 1 {
		t.Fatalf("idempotent got=%+v err=%v puts=%d", got, err, putCalls)
	}
}

func TestReplaceExactADeletesStaleAddressWhenProviderAppends(t *testing.T) {
	records := []Record{
		{ID: "old", Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60},
		{ID: "other", Name: "keep", Type: "TXT", Value: "unchanged", TTL: 300},
	}
	putCalls, deleteCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": records, "total": len(records)})
		case http.MethodPut:
			var body recordsWriteRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Items) != 1 {
				t.Fatalf("put body=%+v err=%v", body, err)
			}
			putCalls++
			added := body.Items[0]
			added.ID = "new"
			records = append(records, added)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			var body []recordDeleteRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 || body[0].Name != "stream" || body[0].Type != "A" || body[0].Address != "1.1.1.1" {
				t.Fatalf("delete body=%+v err=%v", body, err)
			}
			deleteCalls++
			next := records[:0]
			for _, record := range records {
				if record.Name == body[0].Name && record.Type == body[0].Type && record.Address == body[0].Address {
					continue
				}
				next = append(next, record)
			}
			records = next
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", APISecret: "s", ManagedDomain: "example.com"}
	got, err := c.ReplaceExactA(context.Background(), "stream.example.com", "1.1.1.1", "2.2.2.2", 60)
	if err != nil || got.Address != "2.2.2.2" || putCalls != 1 || deleteCalls != 1 {
		t.Fatalf("got=%+v err=%v puts=%d deletes=%d", got, err, putCalls, deleteCalls)
	}
	if len(records) != 2 || records[0].Name != "keep" || records[0].Value != "unchanged" || records[1].Address != "2.2.2.2" {
		t.Fatalf("records=%+v", records)
	}
}
