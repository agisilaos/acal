package app

import "testing"

func TestBuildVersionString(t *testing.T) {
	SetBuildInfo("v1.2.3", "abc123", "2026-02-16T12:00:00Z")
	got := BuildVersionString()
	want := "v1.2.3 (abc123) 2026-02-16T12:00:00Z"
	if got != want {
		t.Fatalf("BuildVersionString() = %q, want %q", got, want)
	}
}
