package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseLabels(t *testing.T) {
	got := ParseLabels("env=prod, dc = fra ,broken,team=platform")
	want := map[string]string{"env": "prod", "dc": "fra", "team": "platform"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLabels() = %#v, want %#v", got, want)
	}
}

func TestValidateLabels(t *testing.T) {
	if err := ValidateLabels(map[string]string{"env": "prod"}, []string{"vm"}); err != nil {
		t.Fatalf("ValidateLabels returned unexpected error: %v", err)
	}
	for name, labels := range map[string]map[string]string{
		"invalid syntax": {"9bad": "x"},
		"reserved":       {"vm": "x"},
		"internal":       {"__name__": "x"},
	} {
		if err := ValidateLabels(labels, []string{"vm"}); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestChooseDurationAndIntPrecedence(t *testing.T) {
	d, err := ChooseDuration(10*time.Second, "5s", time.Second, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	if d != 10*time.Second {
		t.Fatalf("duration = %s, want 10s", d)
	}
	i, err := ChooseInt(25, "10", 1, "page-size")
	if err != nil {
		t.Fatal(err)
	}
	if i != 25 {
		t.Fatalf("int = %d, want 25", i)
	}
}

func TestDefaultReportPathsAndJobWindow(t *testing.T) {
	cfg := Default()
	if cfg.JobCompletedLookupTime != 86400 {
		t.Fatalf("JobCompletedLookupTime = %d, want 86400", cfg.JobCompletedLookupTime)
	}
	for name, path := range map[string]string{
		"CommcellDetails":   cfg.Paths.CommcellDetails,
		"SLA":               cfg.Paths.SLA,
		"Jobs24h":           cfg.Paths.Jobs24h,
		"HealthOverview":    cfg.Paths.HealthOverview,
		"CurrentCapacity":   cfg.Paths.CurrentCapacity,
		"StorageSpaceUsage": cfg.Paths.StorageSpaceUsage,
	} {
		if !strings.HasPrefix(path, "/CustomReportsEngine/rest/reportsplusengine/datasets/") {
			t.Fatalf("%s path = %q, want documented report engine path", name, path)
		}
	}
	if cfg.Paths.Environment != "" {
		t.Fatalf("Environment path = %q, want no default", cfg.Paths.Environment)
	}
}
