package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elohmeier/commvault-exporter/internal/commvault"
	"github.com/elohmeier/commvault-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestExporterRefreshVMModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/VM":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalRecords": 1,
				"pageSize":     1000,
				"vmStatusInfoList": []map[string]any{{
					"name":         "vm-1",
					"strGUID":      "guid-1",
					"vmStatus":     1,
					"vmSize":       2048,
					"vmUsedSpace":  1024,
					"bkpStartTime": 999,
					"bkpEndTime":   999,
					"client":       map[string]any{"clientName": "vm-1"},
					"lastBackupJobInfo": map[string]any{
						"startTime": map[string]any{"time": 111},
						"endTime":   map[string]any{"time": 222},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"dashboard", "jobs", "alerts", "events", "storage", "licensing"}
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_vm_last_backup_end_time_seconds Commvault VM last backup end timestamp.
# TYPE commvault_vm_last_backup_end_time_seconds gauge
commvault_vm_last_backup_end_time_seconds{guid="guid-1",vm="vm-1"} 222
# HELP commvault_vm_last_backup_start_time_seconds Commvault VM last backup start timestamp.
# TYPE commvault_vm_last_backup_start_time_seconds gauge
commvault_vm_last_backup_start_time_seconds{guid="guid-1",vm="vm-1"} 111
# HELP commvault_vm_status Commvault VM backup status code.
# TYPE commvault_vm_status gauge
commvault_vm_status{guid="guid-1",status="1",status_name="protected",vm="vm-1"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_vm_status", "commvault_vm_last_backup_start_time_seconds", "commvault_vm_last_backup_end_time_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestExporterSkipsEnvironmentReportWhenNoEndpointConfigured(t *testing.T) {
	var environmentRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reports/commcell":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "CommCellName"},
					{"name": "Version"},
					{"name": "ReleaseName"},
				},
				"records": [][]any{{"commcell-a", "11.42", "2024E"}},
			})
		case "/reports/sla":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Data Source"},
					{"name": "SLAStatus"},
					{"name": "CurrentCount"},
				},
				"records": [][]any{{"commcell-a", "Met SLA", 1}},
			})
		case "/reports/jobs-24h":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Data Source"},
					{"name": "Name"},
					{"name": "CurrentCount"},
				},
				"records": [][]any{{"commcell-a", "Completed", 1}},
			})
		case "/reports/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Data Source"},
					{"name": "Status"},
					{"name": "Count"},
				},
				"records": [][]any{{"commcell-a", "1_Good", 1}},
			})
		case "/reports/environment":
			atomic.AddInt32(&environmentRequests, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []any{},
				"records": []any{},
				"failures": map[string]any{
					"CacheDB": []any{"Bad Request. Please check the parameters."},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "jobs", "alerts", "events", "storage", "licensing"}
	cfg.Paths.CommcellDetails = "/reports/commcell"
	cfg.Paths.SLA = "/reports/sla"
	cfg.Paths.Jobs24h = "/reports/jobs-24h"
	cfg.Paths.HealthOverview = "/reports/health"
	cfg.Paths.Environment = ""
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&environmentRequests); got != 0 {
		t.Fatalf("environment report requests = %d, want 0", got)
	}
}

func TestExporterStoragePoolInfoIsInfoMetric(t *testing.T) {
	var currentCapacityRequests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/commandcenter/api/StoragePool":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"storagePoolList": []map[string]any{{
					"status":          "Online",
					"statusCode":      0,
					"storagePoolType": 1,
					"totalCapacity":   2048,
					"totalFreeSpace":  1024,
					"storagePoolEntity": map[string]any{
						"storagePoolId":   42,
						"storagePoolName": "pool-a",
					},
					"storagePool": map[string]any{
						"clientGroupName": "group-a",
					},
				}},
			})
		case "/commandcenter/api/V2/StoragePolicy":
			_ = json.NewEncoder(w).Encode(map[string]any{"policies": []any{}, "error": map[string]any{"errorCode": 0}})
		case "/commandcenter/api/MediaAgent":
			_ = json.NewEncoder(w).Encode(map[string]any{"response": []any{}})
		case "/webconsole/api/Library":
			_ = json.NewEncoder(w).Encode(map[string]any{"libraryList": []any{}})
		case "/reports/current-capacity":
			atomic.AddInt32(&currentCapacityRequests, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		case "/reports/storage-space-usage":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "licensing"}
	cfg.Paths.CurrentCapacity = "/reports/current-capacity"
	cfg.Paths.StorageSpaceUsage = "/reports/storage-space-usage"
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&currentCapacityRequests); got != 0 {
		t.Fatalf("current capacity requests = %d, want 0 when only storage is enabled", got)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_storage_pool_info Commvault storage pool metadata.
# TYPE commvault_storage_pool_info gauge
commvault_storage_pool_info{client_group="group-a",pool="pool-a",pool_id="42",status="Online",type="1"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_storage_pool_info"); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateStorageEvents(t *testing.T) {
	events := []commvault.Event{
		{
			Severity:        6,
			EventCodeString: "64:1097",
			Description:     "Storage accelerator is disabled for the client client-a due to the mount path mount-root-a/copy-set-01 is not accessible for 8 attempts",
			TimeSource:      100,
			ClientEntity:    commvault.Entity{ClientName: "client-a"},
		},
		{
			Severity:        6,
			EventCodeString: "64:1097",
			Description:     "Storage accelerator is disabled for the client client-a due to the mount path mount-root-a/copy-set-01 is not accessible for 21 attempts",
			TimeSource:      200,
			ClientName:      "client-a",
		},
		{
			Severity:        6,
			EventCodeString: "64:1097",
			Description:     "description format changed",
			TimeSource:      300,
			ClientName:      "client-b",
		},
		{
			Severity:        9,
			EventCodeString: "74:138",
			Description:     "Mount path went offline",
			TimeSource:      400,
			ClientName:      "media-agent-a",
		},
		{Severity: 6, EventCodeString: "1:2", TimeSource: 500},
	}

	aggregates := aggregateStorageEvents(events)
	if len(aggregates) != 3 {
		t.Fatalf("aggregate count = %d, want 3: %#v", len(aggregates), aggregates)
	}
	key := storageEventKey{
		Code:      "64:1097",
		Type:      "storage_accelerator_inaccessible",
		Severity:  "6",
		Client:    "client-a",
		MountPath: "mount-root-a/copy-set-01",
	}
	if got := aggregates[key]; got.Count != 2 || got.Latest != 200 || got.Attempts != 21 || !got.HasAttempts {
		t.Fatalf("accelerator aggregate = %#v", got)
	}
	malformed := aggregates[storageEventKey{
		Code: "64:1097", Type: "storage_accelerator_inaccessible", Severity: "6", Client: "client-b",
	}]
	if malformed.Count != 1 || malformed.HasAttempts {
		t.Fatalf("malformed aggregate = %#v", malformed)
	}
	if path, attempts, ok := parseStorageAcceleratorDescription("prefix due to the mount path path with spaces/copy is not accessible for 9 attempts suffix"); !ok || path != "path with spaces/copy" || attempts != 9 {
		t.Fatalf("parseStorageAcceleratorDescription = %q, %d, %t", path, attempts, ok)
	}
}

func TestExporterEventsEmitStableAggregates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/api/Events" {
			http.NotFound(w, r)
			return
		}
		from, _ := strconv.ParseInt(r.URL.Query().Get("fromTime"), 10, 64)
		to, _ := strconv.ParseInt(r.URL.Query().Get("toTime"), 10, 64)
		if to-from != int64((24 * time.Hour).Seconds()) {
			t.Fatalf("event window = %d seconds, want 86400", to-from)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commservEvents": []map[string]any{
				{
					"severity":        6,
					"eventCodeString": "64:1097",
					"description":     "Storage accelerator is disabled due to the mount path mount-root-a/copy-set-01 is not accessible for 8 attempts",
					"timeSource":      100,
					"jobId":           1001,
					"id":              2001,
					"clientEntity":    map[string]any{"clientName": "client-a"},
				},
				{
					"severity":        6,
					"eventCodeString": "64:1097",
					"description":     "Storage accelerator is disabled due to the mount path mount-root-a/copy-set-01 is not accessible for 21 attempts",
					"timeSource":      200,
					"jobId":           1002,
					"id":              2002,
					"clientEntity":    map[string]any{"clientName": "client-a"},
				},
			},
		})
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "storage", "licensing"}
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_storage_access_event_count Commvault storage access events in the configured lookup window.
# TYPE commvault_storage_access_event_count gauge
commvault_storage_access_event_count{client="client-a",event_code="64:1097",event_type="storage_accelerator_inaccessible",mount_path="mount-root-a/copy-set-01",severity="6"} 2
# HELP commvault_storage_access_event_last_timestamp_seconds Unix timestamp of the latest matching Commvault storage access event.
# TYPE commvault_storage_access_event_last_timestamp_seconds gauge
commvault_storage_access_event_last_timestamp_seconds{client="client-a",event_code="64:1097",event_type="storage_accelerator_inaccessible",mount_path="mount-root-a/copy-set-01",severity="6"} 200
# HELP commvault_storage_access_event_latest_attempts Attempt count reported by the latest matching Commvault storage access event.
# TYPE commvault_storage_access_event_latest_attempts gauge
commvault_storage_access_event_latest_attempts{client="client-a",event_code="64:1097",event_type="storage_accelerator_inaccessible",mount_path="mount-root-a/copy-set-01",severity="6"} 21
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected),
		"commvault_storage_access_event_count",
		"commvault_storage_access_event_last_timestamp_seconds",
		"commvault_storage_access_event_latest_attempts",
	); err != nil {
		t.Fatal(err)
	}
}

func TestExporterLibraryMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/Library":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": []map[string]any{{
					"entityInfo": map[string]any{
						"id":   101,
						"name": "disk-library-a",
					},
				}},
			})
		case "/webconsole/api/Library/101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"libraryInfo": map[string]any{
					"libraryType": 3,
					"magLibSummary": map[string]any{
						"isOnline":         "Ready",
						"onlineMountPaths": "1 out of 1",
						"numOfMountPath":   1,
					},
					"MountPathList": []map[string]any{{
						"mountPathId":                201,
						"mountPathName":              "[media-agent-a] mount-path-a",
						"status":                     "Ready (Disabled for write)",
						"disabledForNewWrite":        true,
						"mountPathUsedForLogCaching": true,
						"mountPathSummary": map[string]any{
							"libraryId":   101,
							"libraryName": "disk-library-a",
						},
					}},
					"library": map[string]any{
						"libraryId":   101,
						"libraryName": "disk-library-a",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	exporter := New(config.Default(), client, slog.Default())
	if err := exporter.collectLibraries(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		exporter.libraryInfo,
		exporter.libraryMountPaths,
		exporter.libraryReady,
		exporter.mountPathWriteOff,
		exporter.mountPathInfo,
		exporter.mountPathReady,
		exporter.mountPathLogCaching,
	)
	expected := `
# HELP commvault_library_info Commvault library metadata from the library inventory and details APIs.
# TYPE commvault_library_info gauge
commvault_library_info{library="disk-library-a",library_id="101",status="Ready",type="3"} 1
# HELP commvault_library_mount_paths Commvault library mount path count by kind.
# TYPE commvault_library_mount_paths gauge
commvault_library_mount_paths{kind="online",library="disk-library-a",library_id="101"} 1
commvault_library_mount_paths{kind="total",library="disk-library-a",library_id="101"} 1
# HELP commvault_library_ready Whether the Commvault library reports a Ready or Online state.
# TYPE commvault_library_ready gauge
commvault_library_ready{library="disk-library-a",library_id="101"} 1
# HELP commvault_mount_path_disabled_for_new_write Whether the Commvault mount path is disabled for new writes.
# TYPE commvault_mount_path_disabled_for_new_write gauge
commvault_mount_path_disabled_for_new_write{library="disk-library-a",library_id="101",mount_path="[media-agent-a] mount-path-a",mount_path_id="201"} 1
# HELP commvault_mount_path_info Commvault mount path metadata.
# TYPE commvault_mount_path_info gauge
commvault_mount_path_info{library="disk-library-a",library_id="101",mount_path="[media-agent-a] mount-path-a",mount_path_id="201",status="Ready (Disabled for write)"} 1
# HELP commvault_mount_path_ready Whether the Commvault mount path status begins with Ready.
# TYPE commvault_mount_path_ready gauge
commvault_mount_path_ready{library="disk-library-a",library_id="101",mount_path="[media-agent-a] mount-path-a",mount_path_id="201"} 1
# HELP commvault_mount_path_used_for_log_caching Whether the Commvault mount path is used for log caching.
# TYPE commvault_mount_path_used_for_log_caching gauge
commvault_mount_path_used_for_log_caching{library="disk-library-a",library_id="101",mount_path="[media-agent-a] mount-path-a",mount_path_id="201"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected),
		"commvault_library_info",
		"commvault_library_mount_paths",
		"commvault_library_ready",
		"commvault_mount_path_disabled_for_new_write",
		"commvault_mount_path_info",
		"commvault_mount_path_ready",
		"commvault_mount_path_used_for_log_caching",
	); err != nil {
		t.Fatal(err)
	}
}

func TestExporterLibrariesWarnsOnEmptyInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": []any{}})
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	exporter := New(config.Default(), client, logger)
	if err := exporter.collectLibraries(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "library inventory is empty") || !strings.Contains(got, "subcollector=libraries") {
		t.Fatalf("logs = %q", got)
	}
}

func TestExporterLibrariesLimitConcurrencyAndKeepPartialResults(t *testing.T) {
	var active, maximum int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webconsole/api/Library" {
			libraries := make([]map[string]any, 8)
			for index := range libraries {
				libraryID := index + 1
				libraries[index] = map[string]any{
					"libraryType": 3,
					"library": map[string]any{
						"libraryId":   libraryID,
						"libraryName": "library-" + strconv.Itoa(libraryID),
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"libraryList": libraries})
			return
		}
		prefix := "/webconsole/api/Library/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		libraryID, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, prefix))
		if err != nil {
			t.Fatal(err)
		}
		current := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		for {
			seen := atomic.LoadInt32(&maximum)
			if current <= seen || atomic.CompareAndSwapInt32(&maximum, seen, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		if libraryID == 3 {
			http.Error(w, `{"error":"detail unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"libraryInfo": map[string]any{
				"libraryType":   3,
				"magLibSummary": map[string]any{"isOnline": "Ready", "numOfMountPath": 0},
				"library": map[string]any{
					"libraryId":   libraryID,
					"libraryName": "library-" + strconv.Itoa(libraryID),
				},
			},
		})
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	exporter := New(config.Default(), client, slog.Default())
	err = exporter.runSubcollector(context.Background(), "storage", "libraries", exporter.collectLibraries)
	if err == nil || !strings.Contains(err.Error(), "library 3 (library-3)") {
		t.Fatalf("collectLibraries error = %v", err)
	}
	if got := atomic.LoadInt32(&maximum); got != libraryDetailWorkers {
		t.Fatalf("maximum concurrent requests = %d, want %d", got, libraryDetailWorkers)
	}
	if got := testutil.CollectAndCount(exporter.libraryInfo); got != 7 {
		t.Fatalf("library info metric count = %d, want 7", got)
	}
	if got := testutil.ToFloat64(exporter.subcollectorUp.WithLabelValues("storage", "libraries")); got != 0 {
		t.Fatalf("libraries subcollector up = %v, want 0", got)
	}
}

func TestExporterLicensingEmitsAggregateMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/V4/License":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commCellId":  42,
				"edition":     "Commvault",
				"licenseMode": "PRODUCTION",
				"expiryDate":  1893456000,
			})
		case "/reports/current-capacity", "/reports/license-empty":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		case "/commandcenter/api/cr/reportsplusengine/datasets/license-operating/data":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "License ID"},
					{"name": "License"},
					{"name": "Available Total (instances)"},
					{"name": "Permanent Purchased (instances)"},
					{"name": "Term Purchased (instances)"},
					{"name": "Used (instances)"},
					{"name": "EvalExpiryDate"},
					{"name": "Summary"},
				},
				"records": [][]any{{"1001", "Operating Instances", 10, 8, 2, 3, "15 Jan 2030", "30.00%"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "storage"}
	cfg.Paths.CurrentCapacity = "/reports/current-capacity"
	cfg.Paths.LicenseOperatingInstances = "cc:cr/reportsplusengine/datasets/license-operating/data"
	cfg.Paths.LicenseEndpointUsers = "/reports/license-empty"
	cfg.Paths.LicenseHyperscaleStorage = "/reports/license-empty"
	cfg.Paths.LicenseAirGapProtect = "/reports/license-empty"
	cfg.Paths.LicenseDataInsights = "/reports/license-empty"
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_commcell_license_expiry_timestamp_seconds Unix timestamp when the current CommCell license expires; 0 means no expiry was supplied.
# TYPE commvault_commcell_license_expiry_timestamp_seconds gauge
commvault_commcell_license_expiry_timestamp_seconds{commcell_id="42",edition="Commvault",license_mode="PRODUCTION"} 1.893456e+09
# HELP commvault_license_amount Commvault license amount by report, license, unit, and kind.
# TYPE commvault_license_amount gauge
commvault_license_amount{kind="available_total",license="Operating Instances",license_id="1001",report="operating_instances",unit="instances"} 10
commvault_license_amount{kind="permanent_purchased",license="Operating Instances",license_id="1001",report="operating_instances",unit="instances"} 8
commvault_license_amount{kind="term_purchased",license="Operating Instances",license_id="1001",report="operating_instances",unit="instances"} 2
commvault_license_amount{kind="used",license="Operating Instances",license_id="1001",report="operating_instances",unit="instances"} 3
# HELP commvault_license_expiry_timestamp_seconds Unix timestamp of the license expiry reported by Commvault; 0 means no expiry was supplied.
# TYPE commvault_license_expiry_timestamp_seconds gauge
commvault_license_expiry_timestamp_seconds{license="Operating Instances",license_id="1001",report="operating_instances",unit="instances"} 1.8946656e+09
# HELP commvault_license_info Commvault license report metadata.
# TYPE commvault_license_info gauge
commvault_license_info{eval_expiry_date="15 Jan 2030",license="Operating Instances",license_id="1001",report="operating_instances",summary="30.00%",unit="instances"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_commcell_license_expiry_timestamp_seconds", "commvault_license_amount", "commvault_license_expiry_timestamp_seconds", "commvault_license_info"); err != nil {
		t.Fatal(err)
	}
}

func TestExporterLicensingEmitsCurrentCapacityMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/V4/License":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commCellId":  42,
				"edition":     "Commvault",
				"licenseMode": "PRODUCTION",
				"expiryDate":  1893456000,
			})
		case "/reports/current-capacity":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Dial"},
					{"name": "Purchased"},
					{"name": "PermTotal"},
					{"name": "Eval"},
					{"name": "Usage"},
					{"name": "EvalExpiryDate"},
				},
				"records": [][]any{
					{"Backup", 10, 8, 2, 3.5, "15 Jan 2030"},
					{"Backup for Unstructured Data", 0, 0, 0, 5.25, "01 Jan 1970"},
				},
			})
		case "/reports/license-empty":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "storage"}
	cfg.Paths.CurrentCapacity = "/reports/current-capacity"
	cfg.Paths.LicenseOperatingInstances = "/reports/license-empty"
	cfg.Paths.LicenseEndpointUsers = "/reports/license-empty"
	cfg.Paths.LicenseHyperscaleStorage = "/reports/license-empty"
	cfg.Paths.LicenseAirGapProtect = "/reports/license-empty"
	cfg.Paths.LicenseDataInsights = "/reports/license-empty"
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_capacity_usage Commvault capacity usage by dial.
# TYPE commvault_capacity_usage gauge
commvault_capacity_usage{dial="Backup",kind="permanent_total"} 8
commvault_capacity_usage{dial="Backup",kind="purchased"} 10
commvault_capacity_usage{dial="Backup",kind="term_purchased"} 2
commvault_capacity_usage{dial="Backup",kind="usage"} 3.5
commvault_capacity_usage{dial="Backup for Unstructured Data",kind="permanent_total"} 0
commvault_capacity_usage{dial="Backup for Unstructured Data",kind="purchased"} 0
commvault_capacity_usage{dial="Backup for Unstructured Data",kind="term_purchased"} 0
commvault_capacity_usage{dial="Backup for Unstructured Data",kind="usage"} 5.25
# HELP commvault_capacity_license_expiry_timestamp_seconds Unix timestamp of the capacity license expiry reported by Commvault; 0 means no expiry was supplied.
# TYPE commvault_capacity_license_expiry_timestamp_seconds gauge
commvault_capacity_license_expiry_timestamp_seconds{dial="Backup"} 1.8946656e+09
commvault_capacity_license_expiry_timestamp_seconds{dial="Backup for Unstructured Data"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_capacity_usage", "commvault_capacity_license_expiry_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestParseLicenseExpiryDate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  float64
	}{
		{name: "empty", value: "", want: 0},
		{name: "epoch sentinel", value: "01 Jan 1970", want: 0},
		{name: "date in UTC", value: "15 Jan 2030", want: 1894665600},
		{name: "surrounding whitespace", value: " 15 Jan 2030 ", want: 1894665600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLicenseExpiryDate(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("parseLicenseExpiryDate(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
	if _, err := parseLicenseExpiryDate("2030-01-15"); err == nil {
		t.Fatal("parseLicenseExpiryDate malformed date error = nil")
	}
}

func TestExporterCommcellLicenseEmitsZeroExpiry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/api/V4/License" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commCellId":  42,
			"edition":     "Commvault",
			"licenseMode": "PRODUCTION",
			"expiryDate":  0,
		})
	}))
	defer server.Close()

	exporter := newLicensingOnlyExporter(t, server.URL, "/reports/unused")
	if err := exporter.collectCommcellLicense(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter.commcellLicenseExpiry)
	expected := `
# HELP commvault_commcell_license_expiry_timestamp_seconds Unix timestamp when the current CommCell license expires; 0 means no expiry was supplied.
# TYPE commvault_commcell_license_expiry_timestamp_seconds gauge
commvault_commcell_license_expiry_timestamp_seconds{commcell_id="42",edition="Commvault",license_mode="PRODUCTION"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_commcell_license_expiry_timestamp_seconds"); err != nil {
		t.Fatal(err)
	}
}

func TestExporterLicensingReportsCommcellLicenseAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/V4/License":
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case "/reports/empty":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exporter := newLicensingOnlyExporter(t, server.URL, "/reports/empty")
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatal("RefreshOnce error = nil")
	}
	if got := testutil.ToFloat64(exporter.subcollectorUp.WithLabelValues("licensing", "commcell_license")); got != 0 {
		t.Fatalf("commcell_license subcollector up = %v, want 0", got)
	}
}

func TestExporterLicensingReportsMalformedExpiryDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/V4/License":
			_ = json.NewEncoder(w).Encode(map[string]any{"expiryDate": 1893456000})
		case "/reports/license-operating":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "License ID"},
					{"name": "License"},
					{"name": "EvalExpiryDate"},
				},
				"records": [][]any{{"1001", "Operating Instances", "2030-01-15"}},
			})
		case "/reports/empty":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exporter := newLicensingOnlyExporter(t, server.URL, "/reports/empty")
	exporter.cfg.Paths.LicenseOperatingInstances = "/reports/license-operating"
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatal("RefreshOnce error = nil")
	}
	if got := testutil.ToFloat64(exporter.subcollectorUp.WithLabelValues("licensing", "operating_instances")); got != 0 {
		t.Fatalf("operating_instances subcollector up = %v, want 0", got)
	}
}

func TestExporterCurrentCapacityReportsMalformedExpiryDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/V4/License":
			_ = json.NewEncoder(w).Encode(map[string]any{"expiryDate": 1893456000})
		case "/reports/current-capacity":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Dial"},
					{"name": "EvalExpiryDate"},
				},
				"records": [][]any{{"Backup", "2030-01-15"}},
			})
		case "/reports/empty":
			_ = json.NewEncoder(w).Encode(map[string]any{"columns": []any{}, "records": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	exporter := newLicensingOnlyExporter(t, server.URL, "/reports/empty")
	exporter.cfg.Paths.CurrentCapacity = "/reports/current-capacity"
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatal("RefreshOnce error = nil")
	}
	if got := testutil.ToFloat64(exporter.subcollectorUp.WithLabelValues("licensing", "current_capacity")); got != 0 {
		t.Fatalf("current_capacity subcollector up = %v, want 0", got)
	}
	if got := testutil.CollectAndCount(exporter.capacityExpiry); got != 0 {
		t.Fatalf("capacity expiry metric count = %d, want 0 after malformed date", got)
	}
}

func newLicensingOnlyExporter(t *testing.T, baseURL, emptyEndpoint string) *Exporter {
	t.Helper()
	client, err := commvault.NewClient(commvault.Config{BaseURL: baseURL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "storage"}
	cfg.Paths.CurrentCapacity = emptyEndpoint
	cfg.Paths.LicenseOperatingInstances = emptyEndpoint
	cfg.Paths.LicenseEndpointUsers = emptyEndpoint
	cfg.Paths.LicenseHyperscaleStorage = emptyEndpoint
	cfg.Paths.LicenseAirGapProtect = emptyEndpoint
	cfg.Paths.LicenseDataInsights = emptyEndpoint
	return New(cfg, client, slog.Default())
}

func TestExporterSkipsLicensingWhenDisabled(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := commvault.NewClient(commvault.Config{BaseURL: server.URL, AuthToken: "token", PageSize: 1000, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RefreshTimeout = time.Second
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "events", "storage", "licensing"}
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestStatusNameObservedCodes(t *testing.T) {
	tests := map[int]string{
		1:  "protected",
		2:  "not_protected",
		5:  "discovered",
		7:  "backed_up_with_warning",
		8:  "deleted",
		42: "status_42",
	}
	for code, want := range tests {
		if got := statusName(code); got != want {
			t.Fatalf("statusName(%d) = %q, want %q", code, got, want)
		}
	}
}
