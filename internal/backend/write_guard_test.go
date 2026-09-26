package backend

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestRecurringCreationRejectedWithoutNativeAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	be := NewOsaScriptBackend()
	start := time.Now()
	_, err := be.AddEvent(ctx, EventCreateInput{Calendar: "fixture", Title: "fixture", Start: start, End: start.Add(time.Hour), RepeatRule: "daily*3"})
	var rejected *WriteRejectedError
	if !errors.As(err, &rejected) || rejected.Reason != "recurring" {
		t.Fatalf("got %v", err)
	}
	rule := "weekly:mon*3"
	_, err = be.UpdateEvent(ctx, "fixture@1", EventUpdateInput{RepeatRule: &rule})
	if !errors.As(err, &rejected) || rejected.Reason != "recurring" {
		t.Fatalf("got %v", err)
	}
}

func TestNativeWriteAccessPolicy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native script requires macOS")
	}
	lines := append(appleScriptDateHandlers(), writeGuardScriptHandlers()...)
	lines = append(lines, `repeat with statusValue in {0, 1, 2, 4}`, `if my writeAccessCheck(statusValue as integer) is not "ACAL_WRITE_REJECTED:permission" then error "unsafe permission policy"`, `end repeat`, `if my writeAccessCheck(3) is not "" then error "full access rejected"`, `return "ok"`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runUpdateAppleScript(ctx, lines)
	if err != nil || out != "ok\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
