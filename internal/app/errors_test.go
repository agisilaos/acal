package app

import (
	"errors"
	"testing"
)

func TestExitCode(t *testing.T) {
	if code := ExitCode(nil); code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if code := ExitCode(errors.New("x")); code != 1 {
		t.Fatalf("expected 1, got %d", code)
	}
	if code := ExitCode(Wrap(7, errors.New("x"))); code != 7 {
		t.Fatalf("expected 7, got %d", code)
	}
}
