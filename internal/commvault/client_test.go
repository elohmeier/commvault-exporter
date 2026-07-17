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
			var req struct {
				Username string `json:"username"`
				Password string `json:"password"`
				Timeout  int    `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Username != "api" {
				t.Fatalf("login username = %q", req.Username)
			}
			if req.Password != "c2VjcmV0" {
				t.Fatalf("login password = %q, want base64-encoded secret", req.Password)
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

func TestClientGetLicenseInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/api/V4/License" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authtoken"); got != "token" {
			t.Fatalf("Authtoken = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commCellId":         42,
			"commServeIPAddress": "192.0.2.10",
			"licenseIPAddress":   "192.0.2.11",
			"edition":            "Commvault",
			"licenseMode":        "PRODUCTION",
			"serialNumber":       "redacted",
			"registrationCode":   "redacted",
			"expiryDate":         1893456000,
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/commandcenter/api", AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	license, err := client.GetLicenseInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if license.CommCellID != 42 || license.Edition != "Commvault" || license.LicenseMode != "PRODUCTION" || license.ExpiryDate != 1893456000 {
		t.Fatalf("license = %#v", license)
	}
}

func TestClientUsesExistingCommandCenterAPIPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commandcenter/api/VM" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/commandcenter/api", AuthToken: "token", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVMs(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientLoginUsesDataAuthToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/Login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]string{"authToken": "QSDK nested-token"},
			})
		case "/webconsole/api/VM":
			if got := r.Header.Get("Authtoken"); got != "QSDK nested-token" {
				t.Fatalf("Authtoken = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"totalRecords": 0, "vmStatusInfoList": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Username: "api", Password: "secret", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVMs(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientLoginReportsTokenlessResponseDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errorCode":    5,
			"errorMessage": "Invalid login credentials",
			"errList": []map[string]any{{
				"errorCode":    7,
				"errorMessage": "Password is not valid",
			}},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Username: "api", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetVMs(context.Background())
	if err == nil {
		t.Fatal("GetVMs error = nil")
	}
	for _, want := range []string{
		"login response did not contain token",
		"errorCode=5",
		"errorMessage=Invalid login credentials",
		"errList.errorCode=7",
		"errList.errorMessage=Password is not valid",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("GetVMs error = %q, want substring %q", err.Error(), want)
		}
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

func TestClientGetTabularReportsDatasetFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/CustomReportsEngine/rest/reportsplusengine/datasets/environment/data" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"columns":          []any{},
			"records":          []any{},
			"recordsCount":     0,
			"totalRecordCount": 0,
			"failures": map[string]any{
				"CacheDB": []any{"Bad Request. Please check the parameters."},
			},
			"warnings": map[string]any{},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetTabular(context.Background(), "/CustomReportsEngine/rest/reportsplusengine/datasets/environment/data")
	if err == nil {
		t.Fatal("GetTabular error = nil")
	}
	for _, want := range []string{
		"returned failures",
		"CacheDB",
		"Bad Request",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("GetTabular error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestClientGetTabularAllowsWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"columns": []map[string]any{{"name": "Dial"}},
			"records": [][]any{
				{"Backup and Recovery"},
			},
			"recordsCount":     1,
			"totalRecordCount": 1,
			"failures":         map[string]any{},
			"warnings": map[string]any{
				"commcell-example": []any{"Warning: Null value is eliminated by an aggregate or other SET operation."},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.GetTabular(context.Background(), "/report")
	if err != nil {
		t.Fatal(err)
	}
	if resp.RecordsCount != 1 || len(resp.Warnings) != 1 {
		t.Fatalf("GetTabular response = %#v", resp)
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

func TestClientGetEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webconsole/api/Events" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("fromTime"); got != "100" {
			t.Fatalf("fromTime = %q, want 100", got)
		}
		if got := r.URL.Query().Get("toTime"); got != "200" {
			t.Fatalf("toTime = %q, want 200", got)
		}
		if got := r.Header.Get("Authtoken"); got != "token" {
			t.Fatalf("Authtoken = %q", got)
		}
		_, _ = w.Write([]byte(`{
			"commservEvents":[{
				"severity":6,
				"eventCode":"1073742921",
				"eventCodeString":"64:1097",
				"description":"Storage accelerator is disabled due to the mount path mount-root-a\u002fcopy-set-01 is not accessible for 8 attempts",
				"timeSource":150,
				"clientEntity":{"clientId":101,"clientName":"client-a"}
			}]
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.GetEvents(context.Background(), 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.CommservEvents) != 1 {
		t.Fatalf("events = %#v", resp.CommservEvents)
	}
	event := resp.CommservEvents[0]
	if event.EventCodeString != "64:1097" || event.EffectiveClientName() != "client-a" || !strings.Contains(event.Description, "mount-root-a/copy-set-01") {
		t.Fatalf("event = %#v", event)
	}
}

func TestClientGetLibrariesAndDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/webconsole/api/Library":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"libraryList": []map[string]any{{
					"libraryType": 3,
					"status":      "Ready",
					"library": map[string]any{
						"libraryId":   101,
						"libraryName": "disk-library-a",
					},
				}},
			})
		case "/webconsole/api/Library/101":
			if got := r.URL.Query().Get("propertyLevel"); got != "20" {
				t.Fatalf("propertyLevel = %q, want 20", got)
			}
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

	client, err := NewClient(Config{BaseURL: server.URL, AuthToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := client.GetLibraries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries.LibraryList) != 1 || libraries.LibraryList[0].Library.ID != 101 {
		t.Fatalf("libraries = %#v", libraries.LibraryList)
	}
	details, err := client.GetLibraryDetails(context.Background(), 101)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.LibraryInfo.MountPathList) != 1 || details.LibraryInfo.MountPathList[0].Status != "Ready (Disabled for write)" {
		t.Fatalf("details = %#v", details)
	}
}
