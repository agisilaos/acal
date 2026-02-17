package app

import (
	"testing"
	"time"
)

func TestSetReminderMarkerReplacesExisting(t *testing.T) {
	notes := "line1\nacal:reminder=-15m\nline2"
	got := setReminderMarker(notes, -30*time.Minute)
	if got != "line1\nline2\nacal:reminder=-30m0s" {
		t.Fatalf("unexpected notes: %q", got)
	}
}

func TestClearReminderMarker(t *testing.T) {
	notes := "a\nacal:reminder=-15m\nb"
	got := clearReminderMarker(notes)
	if got != "a\nb" {
		t.Fatalf("unexpected notes after clear: %q", got)
	}
}
