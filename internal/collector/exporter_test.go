package collector

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
					"name":        "vm-1",
					"strGUID":     "guid-1",
					"vmStatus":    1,
					"vmSize":      2048,
					"vmUsedSpace": 1024,
					"client":      map[string]any{"clientName": "vm-1"},
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
	cfg.DisabledModules = []string{"dashboard", "jobs", "alerts", "storage"}
	exporter := New(cfg, client, slog.Default())
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)
	expected := `
# HELP commvault_vm_status Commvault VM backup status code.
# TYPE commvault_vm_status gauge
commvault_vm_status{guid="guid-1",status="1",status_name="protected",vm="vm-1"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "commvault_vm_status"); err != nil {
		t.Fatal(err)
	}
}
