package collector

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestFailedModuleRetainsSnapshotUntilStale(t *testing.T) {
	exporter := newReliabilityExporter(t, []string{"dashboard", "jobs", "alerts", "events", "storage", "licensing"})
	now := time.Unix(1_800_000_000, 0)
	exporter.now = func() time.Time { return now }

	first := newDataMetrics(exporter.cfg)
	exporter.useDataMetrics(first)
	if !exporter.runModule(context.Background(), "vm", func(context.Context) error {
		exporter.vmStatus.With(exporter.baseLabels("vm", "vm-a", "guid", "guid-a", "status", "1", "status_name", "protected")).Set(1)
		return nil
	}, first.moduleCollectors("vm")) {
		t.Fatal("initial vm module failed")
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_vm_status Commvault VM backup status code.
# TYPE commvault_vm_status gauge
commvault_vm_status{guid="guid-a",status="1",status_name="protected",vm="vm-a"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_vm_status"); err != nil {
		t.Fatal(err)
	}

	second := newDataMetrics(exporter.cfg)
	exporter.useDataMetrics(second)
	if exporter.runModule(context.Background(), "vm", func(context.Context) error {
		exporter.vmStatus.With(exporter.baseLabels("vm", "partial", "guid", "partial", "status", "2", "status_name", "not_protected")).Set(2)
		return errors.New("refresh failed")
	}, second.moduleCollectors("vm")) {
		t.Fatal("failed vm module reported success")
	}
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_vm_status"); err != nil {
		t.Fatalf("last successful snapshot was not retained: %v", err)
	}

	now = now.Add(exporter.cfg.MaxStale + time.Second)
	if err := testutil.GatherAndCompare(reg, strings.NewReader(""), "commvault_vm_status"); err != nil {
		t.Fatalf("stale snapshot remained visible: %v", err)
	}
}

func TestCoreSnapshotControlsReadiness(t *testing.T) {
	exporter := newReliabilityExporter(t, []string{"dashboard", "jobs", "alerts", "events", "storage"})
	now := time.Unix(1_800_000_000, 0)
	exporter.now = func() time.Time { return now }

	data := newDataMetrics(exporter.cfg)
	exporter.useDataMetrics(data)
	if !exporter.runModule(context.Background(), "vm", func(context.Context) error { return nil }, data.moduleCollectors("vm")) {
		t.Fatal("vm module failed")
	}
	if exporter.runModule(context.Background(), "licensing", func(context.Context) error { return errors.New("optional failure") }, data.moduleCollectors("licensing")) {
		t.Fatal("licensing module reported success")
	}
	status := exporter.cacheStatus(now)
	if !status.Ready || status.Stale {
		t.Fatalf("status = %+v, want ready with failed optional module", status)
	}
	for _, module := range status.Modules {
		if module.Module == "vm" && !module.Required {
			t.Fatal("vm module is not marked required")
		}
		if module.Module == "licensing" && module.Required {
			t.Fatal("licensing module is marked required")
		}
	}
}

func TestRemainingOptionalModuleBecomesRequired(t *testing.T) {
	exporter := newReliabilityExporter(t, []string{"vm", "dashboard", "jobs", "alerts", "events", "storage"})
	now := time.Unix(1_800_000_000, 0)
	exporter.now = func() time.Time { return now }
	if exporter.cacheStatus(now).Ready {
		t.Fatal("exporter is ready before the remaining module succeeds")
	}
	data := newDataMetrics(exporter.cfg)
	exporter.useDataMetrics(data)
	if !exporter.runModule(context.Background(), "licensing", func(context.Context) error { return nil }, data.moduleCollectors("licensing")) {
		t.Fatal("licensing module failed")
	}
	if status := exporter.cacheStatus(now); !status.Ready {
		t.Fatalf("status = %+v, want remaining module to control readiness", status)
	}
}

func TestNoEnabledModulesCompletesAsReady(t *testing.T) {
	exporter := newReliabilityExporter(t, append([]string(nil), moduleNames...))
	exporter.now = time.Now
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := exporter.cacheStatus(time.Now()); !status.Ready {
		t.Fatalf("status = %+v, want successful no-op refresh to be ready", status)
	}
}

func TestRetryDelayBackoffAndCap(t *testing.T) {
	exporter := newReliabilityExporter(t, nil)
	exporter.jitter = func(delay time.Duration) time.Duration { return delay }
	exporter.cfg.RefreshInterval = time.Minute
	wants := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, time.Minute}
	for i, want := range wants {
		exporter.consecutiveErrs = i + 1
		if got := exporter.retryDelay(); got != want {
			t.Fatalf("retry %d delay = %s, want %s", i+1, got, want)
		}
	}
}

func TestSchedulerRetriesFailedInitialRefresh(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, `{"error":"temporary"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshInterval = time.Hour
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"dashboard", "jobs", "alerts", "events", "storage", "licensing"}
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	exporter.retryBase = 5 * time.Millisecond
	exporter.jitter = func(delay time.Duration) time.Duration { return delay }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exporter.Start(ctx)
	defer exporter.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if exporter.cacheStatus(time.Now()).Ready {
			if requests.Load() < 2 {
				t.Fatalf("requests = %d, want at least two", requests.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exporter did not become ready after retry")
}

func TestRefreshRunsModulesConcurrently(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		if r.URL.Path == "/webconsole/api/VM" {
			_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"jobs", "alerts", "events", "storage", "licensing"}
	cfg.Paths.CommcellDetails = "/commcell"
	cfg.Paths.SLA = "/sla"
	cfg.Paths.Jobs24h = "/jobs-24h"
	cfg.Paths.HealthOverview = "/health-overview"
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := maximum.Load(); got < 2 {
		t.Fatalf("maximum concurrent module requests = %d, want at least 2", got)
	}
}

func newReliabilityExporter(t *testing.T, disabled []string) *Exporter {
	t.Helper()
	client, err := commvault.NewClient(commvault.Config{BaseURL: "https://commvault.example.com", AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DisabledModules = disabled
	return New(cfg, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
