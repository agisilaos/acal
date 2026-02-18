package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type backendContextError struct {
	Phase    string
	Kind     string
	Deadline *time.Time
	Err      error
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
	var be *backendContextError
	if !errors.As(err, &be) || be == nil {
		return nil
	}
	meta := map[string]any{
		"phase": be.Phase,
		"kind":  be.Kind,
	}
	if be.Deadline != nil {
		meta["deadline"] = be.Deadline.Format(time.RFC3339)
	}
	return meta
}
