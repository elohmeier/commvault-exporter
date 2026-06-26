package commvault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientLoginAndGetVMsWithPaging(t *testing.T) {
	var loginSeen bool
	var paging []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/Login":
			loginSeen = true
			if r.Method != http.MethodPost {
				t.Fatalf("login method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "QSDK test-token"})
		case "/webconsole/api/VM":
			if got := r.Header.Get("Authtoken"); got != "QSDK test-token" {
				t.Fatalf("Authtoken = %q", got)
			}
			paging = append(paging, r.Header.Get("PagingInfo"))
			switch r.Header.Get("PagingInfo") {
			case "0,1":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"totalRecords": 2,
					"pageSize":     1,
					"vmStatusInfoList": []map[string]any{{
						"name":       "vm-1",
						"strGUID":    "guid-1",
						"vmStatus":   1,
						"bkpEndTime": 100,
					}},
				})
			case "1,1":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"totalRecords": 2,
					"pageSize":     1,
					"vmStatusInfoList": []map[string]any{{
						"name":     "vm-2",
						"strGUID":  "guid-2",
						"vmStatus": 2,
					}},
				})
			default:
				t.Fatalf("unexpected PagingInfo %q", r.Header.Get("PagingInfo"))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:  server.URL,
		Username: "api",
		Password: "secret",
		PageSize: 1,
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	vms, err := client.GetVMs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !loginSeen {
		t.Fatal("login was not called")
	}
	if len(vms) != 2 || vms[0].Name != "vm-1" || vms[1].Name != "vm-2" {
		t.Fatalf("VMs = %#v", vms)
	}
	if strings.Join(paging, ";") != "0,1;1,1" {
		t.Fatalf("paging = %#v", paging)
	}
}

func TestClientUsesExistingWebconsoleAPIPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/api/VM" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/webconsole/api", AuthToken: "token", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVMs(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAPIURLSupportsCommandCenterAndRawPaths(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://commvault.example.com/root", AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	cc := client.apiURL("cc:V2/StoragePolicy", nil)
	if got, want := cc.String(), "https://commvault.example.com/root/commandcenter/api/V2/StoragePolicy"; got != want {
		t.Fatalf("commandcenter URL = %s, want %s", got, want)
	}
	raw := client.apiURL("/cr/reportsplusengine/datasets/id/data?cache=true", nil)
	if got, want := raw.String(), "https://commvault.example.com/root/cr/reportsplusengine/datasets/id/data?cache=true"; got != want {
		t.Fatalf("raw URL = %s, want %s", got, want)
	}

	client, err = NewClient(Config{BaseURL: "https://commvault.example.com/webconsole/api", AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	cc = client.apiURL("cc:StoragePool", nil)
	if got, want := cc.String(), "https://commvault.example.com/commandcenter/api/StoragePool"; got != want {
		t.Fatalf("commandcenter URL from webconsole base = %s, want %s", got, want)
	}

	raw = client.apiURL("/cr/reportsplusengine/datasets/id/data", nil)
	if got, want := raw.String(), "https://commvault.example.com/cr/reportsplusengine/datasets/id/data"; got != want {
		t.Fatalf("raw URL from webconsole base = %s, want %s", got, want)
	}
}

func TestClientBearerAuthMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Authtoken"); got != "" {
			t.Fatalf("Authtoken = %q, want empty", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token", AuthMode: "bearer", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVMs(context.Background()); err != nil {
		t.Fatal(err)
	}
}
