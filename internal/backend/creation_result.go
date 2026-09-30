package backend

import "fmt"

// CreationOutcomeError means native creation may have completed without a usable
// result. Callers must inspect Calendar before retrying and must not record history.
type CreationOutcomeError struct{ Err error }

func (e *CreationOutcomeError) Error() string {
	return fmt.Sprintf("creation outcome unknown; Calendar may have changed: %v", e.Err)
}

func (e *CreationOutcomeError) Unwrap() error { return e.Err }
