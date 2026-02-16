package app

import (
	"errors"
	"testing"

	"github.com/agis/acal/internal/contract"
)

func TestBuildSetupResultReady(t *testing.T) {
	checks := []contract.DoctorCheck{
		{Name: "osascript", Status: "ok"},
		{Name: "calendar_access", Status: "ok"},
		{Name: "calendar_db", Status: "ok"},
		{Name: "calendar_db_read", Status: "ok"},
	}
	res := buildSetupResult(checks, nil, "osascript")
	if !res.Ready {
		t.Fatalf("expected ready result")
	}
	if res.Degraded {
		t.Fatalf("expected non-degraded result")
	}
	if len(res.NextSteps) == 0 {
		t.Fatalf("expected next steps")
	}
}

func TestBuildSetupResultCriticalFailure(t *testing.T) {
	checks := []contract.DoctorCheck{
		{Name: "osascript", Status: "ok"},
		{Name: "calendar_access", Status: "fail"},
	}
	res := buildSetupResult(checks, errors.New("denied"), "osascript")
	if res.Ready {
		t.Fatalf("expected not ready result")
	}
	if len(res.NextSteps) == 0 {
		t.Fatalf("expected remediation steps")
	}
}

func TestBuildSetupResultDegradedOnly(t *testing.T) {
	checks := []contract.DoctorCheck{
		{Name: "osascript", Status: "ok"},
		{Name: "calendar_access", Status: "ok"},
		{Name: "calendar_db_read", Status: "fail"},
	}
	res := buildSetupResult(checks, errors.New("db denied"), "osascript")
	if !res.Ready {
		t.Fatalf("expected ready result when only DB read is unavailable")
	}
	if !res.Degraded {
		t.Fatalf("expected degraded=true")
	}
}
