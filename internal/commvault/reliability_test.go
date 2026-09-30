package commvault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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

func TestRetryOnlyFailedJobPage(t *testing.T) {
	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.Header.Get("offset"))
		if len(offsets) == 2 {
			http.Error(w, "temporary", 503)
			return
		}
		id := 1
		if r.Header.Get("offset") == "1" {
			id = 2
		}
		json.NewEncoder(w).Encode(map[string]any{"totalRecordsWithoutPaging": 2, "jobs": []any{map[string]any{"jobSummary": map[string]any{"jobId": id}}}})
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	c.retryWait = func(context.Context, time.Duration) error { return nil }
	jobs, err := c.GetJobs(context.Background(), 86400)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || strings.Join(offsets, ",") != "0,1,1" {
		t.Fatalf("jobs=%v pages=%v", jobs, offsets)
	}
}

func TestTransientRetryClassificationAndLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want int
	}{
		{"forbidden", 403, `{}`, 1},
		{"invalid_json", 200, `{`, 1},
		{"rate_limit", 429, `{}`, 2},
		{"unavailable", 503, `{}`, 2},
		{"unsupported", 501, `{}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.code)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			c.retryWait = func(context.Context, time.Duration) error { return nil }
			if _, err := c.GetTabular(context.Background(), "/data"); err == nil {
				t.Fatal("expected failure")
			}
			if got := calls.Load(); got != int32(tc.want) {
				t.Fatalf("requests=%d want %d", got, tc.want)
			}
		})
	}
}

func TestRetryAfterAndCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		http.Error(w, "busy", 429)
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token", Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.retryWait = func(ctx context.Context, d time.Duration) error {
		if d != 2*time.Second {
			t.Fatalf("retry delay=%s", d)
		}
		cancel()
		return waitRetry(ctx, d)
	}
	if _, err := c.GetTabular(ctx, "/data"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d", calls.Load())
	}
	now := time.Now().Truncate(time.Second)
	if got := parseRetryAfter(now.Add(time.Minute).UTC().Format(http.TimeFormat), now); got != time.Minute {
		t.Fatalf("HTTP date=%s", got)
	}
	if got := parseRetryAfter("9223372036854775807", now); got != 0 {
		t.Fatalf("overflow=%s", got)
	}
}

func TestFailedDecodeDoesNotMutateDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"records":[["partial"]],`) }))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	var resp TabularResponse
	_, _, err = c.doOnce(context.Background(), http.MethodGet, "/data", nil, nil, nil, &resp, true)
	if err == nil {
		t.Fatal("expected invalid JSON")
	}
	if len(resp.Records) != 0 {
		t.Fatalf("partial response leaked: %+v", resp)
	}
}

func TestReadTimeoutRetriesSameRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"records":[["partial"]],`)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		io.WriteString(w, `{"columns":[{"name":"value"}],"records":[["complete"]]}`)
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token", Timeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c.retryWait = func(context.Context, time.Duration) error { return nil }
	response, err := c.GetTabular(context.Background(), "/data")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(response.Records) != 1 || response.Records[0][0] != "complete" {
		t.Fatalf("calls=%d response=%+v", calls.Load(), response)
	}
}

func TestMixedAndWrappedFailuresDoNotRetryPermanentErrors(t *testing.T) {
	transient := APIError{StatusCode: 503}
	permanent := APIError{StatusCode: 403}
	if IsTransient(fmt.Errorf("collector: %w", errors.Join(transient, permanent))) {
		t.Fatal("mixed failure retried")
	}
	if !IsTransient(fmt.Errorf("collector: %w", errors.Join(transient, context.DeadlineExceeded))) {
		t.Fatal("transient group not retried")
	}
	if !IsTransient(fmt.Errorf("dial: %w", syscall.ECONNREFUSED)) {
		t.Fatal("connection refusal not retried")
	}
}
