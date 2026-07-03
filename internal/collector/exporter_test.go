package collector

import (
	"context"
	"encoding/json"
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
	cfg.DisabledModules = []string{"dashboard", "jobs", "alerts", "storage", "licensing"}
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
	cfg.DisabledModules = []string{"vm", "jobs", "alerts", "storage", "licensing"}
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
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "licensing"}
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

func TestExporterLicensingEmitsAggregateMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
				"records": [][]any{{"100032", "Operating Instances", 20, 15, 5, 12, "01 Jan 1970", "60.00%"}},
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
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "storage"}
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
# HELP commvault_license_amount Commvault license amount by report, license, unit, and kind.
# TYPE commvault_license_amount gauge
commvault_license_amount{kind="available_total",license="Operating Instances",license_id="100032",report="operating_instances",unit="instances"} 20
commvault_license_amount{kind="permanent_purchased",license="Operating Instances",license_id="100032",report="operating_instances",unit="instances"} 15
commvault_license_amount{kind="term_purchased",license="Operating Instances",license_id="100032",report="operating_instances",unit="instances"} 5
commvault_license_amount{kind="used",license="Operating Instances",license_id="100032",report="operating_instances",unit="instances"} 12
# HELP commvault_license_info Commvault license report metadata.
# TYPE commvault_license_info gauge
commvault_license_info{eval_expiry_date="01 Jan 1970",license="Operating Instances",license_id="100032",report="operating_instances",summary="60.00%",unit="instances"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_license_amount", "commvault_license_info"); err != nil {
		t.Fatal(err)
	}
}

func TestExporterLicensingEmitsCurrentCapacityCompatibilityMetric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reports/current-capacity":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]any{
					{"name": "Dial"},
					{"name": "Purchased"},
					{"name": "PermTotal"},
					{"name": "Eval"},
					{"name": "Usage"},
				},
				"records": [][]any{{"Backup", 100, 90, 10, 33.5}},
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
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "storage"}
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
commvault_capacity_usage{dial="Backup",kind="evaluation"} 10
commvault_capacity_usage{dial="Backup",kind="permanent_total"} 90
commvault_capacity_usage{dial="Backup",kind="purchased"} 100
commvault_capacity_usage{dial="Backup",kind="usage"} 33.5
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_capacity_usage"); err != nil {
		t.Fatal(err)
	}
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
	cfg.DisabledModules = []string{"vm", "dashboard", "jobs", "alerts", "storage", "licensing"}
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
