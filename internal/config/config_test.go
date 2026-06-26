package config

import (
	"reflect"
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
