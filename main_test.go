package main

import (
	"io"
	"testing"
)

func TestRunRejectsInvalidEventLookbackEnvironment(t *testing.T) {
	t.Setenv("COMMVAULT_AUTH_TOKEN", "token")
	t.Setenv("COMMVAULT_EVENT_LOOKBACK", "not-a-duration")
	if got := run([]string{"-url", "https://commvault.example.com"}, io.Discard, io.Discard); got != 1 {
		t.Fatalf("run exit code = %d, want 1", got)
	}
}

func TestRunRejectsInvalidEventLookbackFlag(t *testing.T) {
	if got := run([]string{"-event-lookback", "not-a-duration"}, io.Discard, io.Discard); got != 2 {
		t.Fatalf("run exit code = %d, want 2", got)
	}
}
