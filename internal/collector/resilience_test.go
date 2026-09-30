package collector

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elohmeier/commvault-exporter/internal/commvault"
	"github.com/elohmeier/commvault-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestReadinessAndMetricsSurviveRemoteOutageAndExpiry(t *testing.T) {
	var unavailable atomic.Bool
	entered := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		io.WriteString(w, `{"totalRecords":1,"vmStatusInfoList":[{"name":"vm-a","strGUID":"a","vmStatus":1}]}`)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.DisabledModules = []string{"dashboard", "jobs", "alerts", "events", "storage", "licensing"}
	cfg.Timeout = 40 * time.Millisecond
	cfg.RefreshTimeout = 100 * time.Millisecond
	cfg.MaxStale = time.Second
	e := resilienceExporter(t, server.URL, cfg)
	now := time.Now()
	e.now = func() time.Time { return now }
	reg := prometheus.NewRegistry()
	reg.MustRegister(e)
	checkReady := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		e.ReadyHandler(w, httptest.NewRequest("GET", "/readyz", nil))
		if w.Code != want {
			t.Fatalf("ready status %d: %s", w.Code, w.Body.String())
		}
		var status cacheStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Ready != (want == 200) {
			t.Fatalf("HTTP and JSON disagree: %+v", status)
		}
	}
	checkReady(200) // No successful remote snapshot is needed, including cold start.
	unavailable.Store(true)
	done := make(chan error, 1)
	go func() { done <- e.RefreshOnce(context.Background()) }()
	<-entered
	checkReady(200)
	metrics := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	if metrics.Code != 200 || !strings.Contains(metrics.Body.String(), "commvault_up 0") {
		t.Fatalf("metrics unavailable during request: %s", metrics.Body.String())
	}
	if err := <-done; err == nil {
		t.Fatal("expected remote failure")
	}
	unavailable.Store(false)
	if err := e.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.CollectAndCount(e, "commvault_vm_info"); got != 1 {
		t.Fatalf("VMs=%d", got)
	}
	unavailable.Store(true)
	if err := e.RefreshOnce(context.Background()); err == nil {
		t.Fatal("expected remote failure")
	}
	checkReady(200)
	if got := testutil.CollectAndCount(e, "commvault_vm_info"); got != 1 {
		t.Fatalf("fresh cached VMs=%d", got)
	}
	now = now.Add(2 * time.Second)
	checkReady(200)
	if got := testutil.CollectAndCount(e, "commvault_vm_info"); got != 0 {
		t.Fatalf("expired VMs=%d", got)
	}
	if s := e.cacheStatus(now); !s.Stale || s.CacheReady {
		t.Fatalf("cache looks healthy: %+v", s)
	}
	unavailable.Store(false)
	if err := e.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.CollectAndCount(e, "commvault_vm_info"); got != 1 {
		t.Fatalf("recovered VMs=%d", got)
	}
	e.Stop()
	checkReady(503)
}

func TestSchedulerRetriesOnlyFailedModuleAndKeepsHealthySchedule(t *testing.T) {
	var vms, jobs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/VM") {
			vms.Add(1)
			io.WriteString(w, `{"totalRecords":0}`)
			return
		}
		jobs.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.DisabledModules = []string{"dashboard", "alerts", "events", "storage", "licensing"}
	cfg.RefreshInterval = time.Hour
	cfg.RefreshTimeout = 30 * time.Millisecond
	e := resilienceExporter(t, server.URL, cfg)
	e.retryBase = 5 * time.Millisecond
	e.jitter = func(d time.Duration) time.Duration { return d }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if jobs.Load() >= 3 && vms.Load() == 1 {
			if got := testutil.ToFloat64(e.up.WithLabelValues()); got != 0 {
				t.Fatalf("up=%v despite failed jobs", got)
			}
			cancel()
			if e.cacheStatus(time.Now()).Ready {
				t.Fatal("ready after scheduler context cancellation")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("requests: VM=%d Job=%d", vms.Load(), jobs.Load())
}

func TestPermanentFailureUsesNormalInterval(t *testing.T) {
	e := newReliabilityExporter(t, nil)
	now := time.Now()
	e.now = func() time.Time { return now }
	for _, err := range []error{commvault.APIError{StatusCode: 403}, io.ErrNoProgress} {
		e.runModule(context.Background(), "vm", func(context.Context) error { return err }, nil)
		if got := e.moduleStates["vm"].NextAttempt.Sub(now); got != e.cfg.RefreshInterval {
			t.Fatalf("permanent retry delay=%s", got)
		}
	}
	e.runModule(context.Background(), "vm", func(context.Context) error { return commvault.APIError{StatusCode: 429, RetryAfter: time.Hour} }, nil)
	if got := e.moduleStates["vm"].NextAttempt.Sub(now); got != time.Hour {
		t.Fatalf("Retry-After ignored: %s", got)
	}
}

func TestStoragePublishesSuccessfulSiblingsAndExpiresFailedSnapshot(t *testing.T) {
	var failLibraries atomic.Bool
	var poolVersion atomic.Int32
	poolVersion.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/StoragePool"):
			json.NewEncoder(w).Encode(map[string]any{"storagePoolList": []any{map[string]any{"storagePoolEntity": map[string]any{"storagePoolId": poolVersion.Load(), "storagePoolName": "pool"}}}})
		case strings.HasSuffix(r.URL.Path, "/Library"):
			if failLibraries.Load() {
				http.Error(w, "forbidden", 403)
				return
			}
			io.WriteString(w, `{"libraryList":[{"library":{"libraryId":1,"libraryName":"lib"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/Library/1"):
			io.WriteString(w, `{"libraryInfo":{"library":{"libraryId":1,"libraryName":"lib"},"magLibSummary":{"isOnline":"Ready"}}}`)
		default:
			io.WriteString(w, `{"columns":[],"records":[]}`)
		}
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "licensing"}
	cfg.MaxStale = time.Second
	e := resilienceExporter(t, server.URL, cfg)
	now := time.Now()
	e.now = func() time.Time { return now }
	if err := e.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.CollectAndCount(e, "commvault_library_info"); got != 1 {
		t.Fatalf("libraries=%d", got)
	}
	now = now.Add(2 * time.Second)
	failLibraries.Store(true)
	poolVersion.Store(2)
	if err := e.RefreshOnce(context.Background()); err == nil {
		t.Fatal("expected library failure")
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(e)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "commvault_storage_pool_info" {
			for _, m := range family.Metric {
				for _, label := range m.Label {
					if label.GetName() == "pool_id" && label.GetValue() != "2" {
						t.Fatalf("stale sibling: %v", m)
					}
				}
			}
		}
	}
	if got := testutil.CollectAndCount(e, "commvault_storage_pool_info"); got != 1 {
		t.Fatalf("pools=%d", got)
	}
	if got := testutil.CollectAndCount(e, "commvault_library_info"); got != 0 {
		t.Fatalf("expired libraries=%d", got)
	}
	if got := testutil.ToFloat64(e.subcollectorCacheStale.WithLabelValues("storage", "libraries")); got != 1 {
		t.Fatalf("library stale=%v", got)
	}
	if !e.cacheStatus(now).Ready {
		t.Fatal("remote failure affected readiness")
	}
}

func resilienceExporter(t *testing.T, url string, cfg config.Config) *Exporter {
	t.Helper()
	client, err := commvault.NewClient(commvault.Config{BaseURL: url, AuthToken: "token", Timeout: cfg.Timeout})
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSlowModuleDoesNotDelayHealthyModule(t *testing.T) {
	var vms atomic.Int32
	jobsStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/VM") {
			vms.Add(1)
			io.WriteString(w, `{"totalRecords":0}`)
			return
		}
		select {
		case jobsStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.DisabledModules = []string{"dashboard", "alerts", "events", "storage", "licensing"}
	cfg.RefreshInterval = 20 * time.Millisecond
	cfg.RefreshTimeout = time.Second
	cfg.Timeout = time.Second
	e := resilienceExporter(t, server.URL, cfg)
	e.Start(context.Background())
	defer e.Stop()
	<-jobsStarted
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if vms.Load() >= 3 {
			if got := testutil.ToFloat64(e.up.WithLabelValues()); got != 0 {
				t.Fatalf("up=%v before first jobs completion", got)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("healthy VM collector blocked behind jobs: %d requests", vms.Load())
}
