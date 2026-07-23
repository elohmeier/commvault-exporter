package commvault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCapsConcurrentRequests(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.GetTabular(context.Background(), "/data")
			errs <- err
		}()
	}
	for range maxConcurrentRequests {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("four requests did not start concurrently")
		}
	}
	select {
	case <-entered:
		t.Fatal("more than four requests entered the server")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got != maxConcurrentRequests {
		t.Fatalf("maximum concurrent requests = %d, want %d", got, maxConcurrentRequests)
	}
}

func TestConcurrentRequestsShareOneLogin(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/Login":
			logins.Add(1)
			time.Sleep(10 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "shared-token"})
		case "/data":
			if r.Header.Get("Authtoken") != "shared-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.GetTabular(context.Background(), "/data")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := logins.Load(); got != 1 {
		t.Fatalf("login requests = %d, want 1", got)
	}
}

func TestUnauthorizedRequestRetriesOnlyOnce(t *testing.T) {
	var requests atomic.Int32
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webconsole/api/Login" {
			logins.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "new-token"})
			return
		}
		requests.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "secret", AuthToken: "old-token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetTabular(context.Background(), "/data"); err == nil {
		t.Fatal("unauthorized request returned nil error")
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("data requests = %d, want 2", got)
	}
	if got := logins.Load(); got != 1 {
		t.Fatalf("login requests = %d, want 1", got)
	}
}
