package spaceship

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

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
		var body []Record
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
			t.Fatalf("unexpected put body: %+v err=%v", body, err)
		}
		_, _ = w.Write([]byte(`{}`))
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
				if r.URL.Query().Get("take") != "100" || r.URL.Query().Get("skip") != "0" {
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
