package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBackendErrorMetaFromDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := annotateBackendError(ctx, "backend.list_events", context.DeadlineExceeded)
	meta := backendErrorMeta(err)
	if meta == nil {
		t.Fatalf("expected metadata for annotated timeout")
	}
	if meta["phase"] != "backend.list_events" || meta["kind"] != "timeout" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if _, ok := meta["deadline"]; !ok {
		t.Fatalf("expected deadline in metadata: %+v", meta)
	}
}

func TestBackendErrorMetaNilForGenericError(t *testing.T) {
	meta := backendErrorMeta(context.Canceled)
	if meta != nil {
		t.Fatalf("did not expect metadata for unannotated error: %+v", meta)
	}
}

func TestBackendContextErrorMessageContainsPhase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := annotateBackendError(ctx, "backend.add_event", context.DeadlineExceeded)
	if !strings.Contains(err.Error(), "backend.add_event timed out") {
		t.Fatalf("expected phase-aware message, got: %q", err.Error())
	}
}
