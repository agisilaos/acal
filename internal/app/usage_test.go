package app

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestUsageBoundaryPreservesRuntimeErrors(t *testing.T) {
	for _, code := range []int{1, 4, 6, 7} {
		root := NewRootCommand()
		failure := errors.New("unknown command: runtime failure")
		var want error = failure
		if code != 1 {
			want = Wrap(code, failure)
		}
		root.AddCommand(&cobra.Command{Use: "failure", RunE: func(*cobra.Command, []string) error { return want }})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		if err := executeCommand(root, []string{"failure"}); err != want || ExitCode(err) != code {
			t.Fatalf("runtime code %d changed: %v (%d)", code, err, ExitCode(err))
		}
	}
}
