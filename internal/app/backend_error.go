package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agis/acal/internal/backend"
)

type backendContextError struct {
	Phase    string
	Kind     string
	Deadline *time.Time
	Err      error
}

// An uncertain failure cannot establish whether Calendar completed a creation.
// Retain the request so every creation consumer can direct safe inspection.
type creationAttemptError struct {
	Err   error
	Input backend.EventCreateInput
}

func (e *creationAttemptError) Error() string { return e.Err.Error() }
func (e *creationAttemptError) Unwrap() error { return e.Err }

func creationInspectionHint(err error) string {
	var creation *creationAttemptError
	if !errors.As(err, &creation) {
		return ""
	}
	in := creation.Input
	return fmt.Sprintf("Inspect Calendar before retrying; the event may already have been created. Check calendar %q, title %q, start %s, and end %s", in.Calendar, in.Title, in.Start.Format(time.RFC3339), in.End.Format(time.RFC3339))
}

func (e *backendContextError) Error() string {
	if e == nil {
		return "backend error"
	}
	switch e.Kind {
	case "timeout":
		if e.Deadline != nil {
			return fmt.Sprintf("%s timed out after deadline %s: %v", e.Phase, e.Deadline.Format(time.RFC3339), e.Err)
		}
		return fmt.Sprintf("%s timed out: %v", e.Phase, e.Err)
	case "canceled":
		return fmt.Sprintf("%s canceled: %v", e.Phase, e.Err)
	default:
		return e.Err.Error()
	}
}

func (e *backendContextError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func annotateBackendError(ctx context.Context, phase string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		var dl *time.Time
		if deadline, ok := ctx.Deadline(); ok {
			deadline = deadline.UTC()
			dl = &deadline
		}
		return &backendContextError{
			Phase:    phase,
			Kind:     "timeout",
			Deadline: dl,
			Err:      err,
		}
	}
	if errors.Is(err, context.Canceled) {
		return &backendContextError{
			Phase: phase,
			Kind:  "canceled",
			Err:   err,
		}
	}
	return err
}

func backendErrorMeta(err error) map[string]any {
	var rejected *backend.WriteRejectedError
	if errors.As(err, &rejected) {
		return map[string]any{"kind": "write_rejected", "reason": rejected.Reason, "applied": false}
	}
	var outcome *backend.UpdateOutcomeError
	if errors.As(err, &outcome) {
		meta := map[string]any{"phase": "backend.update_event", "kind": "update_outcome_unknown", "verified": false}
		if outcome.Applied {
			meta["kind"] = "update_applied_unverified"
			meta["applied"] = true
		}
		return meta
	}

	var be *backendContextError
	var nativeCreation *backend.CreationOutcomeError
	var meta map[string]any
	if errors.As(err, &be) {
		meta = map[string]any{"phase": be.Phase, "kind": be.Kind}
		if be.Deadline != nil {
			meta["deadline"] = be.Deadline.Format(time.RFC3339)
		}
	} else if errors.As(err, &nativeCreation) {
		meta = map[string]any{"phase": "backend.add_event", "kind": "creation_outcome_unknown"}
	} else {
		return nil
	}
	var creation *creationAttemptError
	if errors.As(err, &creation) {
		meta["outcome"], meta["verified"] = "unknown", false
		meta["calendar"], meta["title"] = creation.Input.Calendar, creation.Input.Title
		meta["start"], meta["end"] = creation.Input.Start.Format(time.RFC3339), creation.Input.End.Format(time.RFC3339)
	}
	return meta
}

// Shared by single commands and per-row batch errors.
func writeRejectionHint(reason string) string {
	switch reason {
	case "permission":
		return "Enable Full Access for the invoking app in System Settings > Privacy & Security > Calendars, then retry."
	case "unclassified":
		return "Re-fetch the event ID and check Calendar access. acal could not establish a unique independent event; nothing was changed."
	case "legacy_history":
		return "Inspect the history entry and restore it manually in Calendar.app. acal cannot prove the old snapshot represents an independent event; both stacks are unchanged."
	default:
		return "Use Calendar.app for recurring-event changes; acal left the event and history unchanged."
	}
}
