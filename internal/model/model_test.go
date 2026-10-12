package model

import "testing"

func TestFailed(t *testing.T) {
	for conclusion, want := range map[string]bool{
		"success":         false,
		"cancelled":       false,
		"skipped":         false,
		"unknown":         false,
		"neutral":         false,
		"stale":           false,
		"action_required": false,
		"failure":         true,
		"timed_out":       true,
		"startup_failure": true,
		"added_later":     true,
		"":                true,
	} {
		if got := Failed(conclusion); got != want {
			t.Errorf("Failed(%q) = %v, want %v", conclusion, got, want)
		}
	}
}
