package app

import (
	"bytes"
	"github.com/agis/acal/internal/output"
	"testing"
)

func TestNativeLocalErrorsIgnoreProductionConfiguration(t *testing.T) {
	for _, mode := range []string{"plain", "jsonl", "nonsense"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ACAL_OUTPUT", mode)
			t.Setenv("ACAL_CONFIG", "/does/not/exist.toml")
			root := NewRootCommand()
			args := []string{"native", "events", "update", "fixture"}
			if got := errorOutputMode(root, args); got != output.ModeJSON {
				t.Fatalf("mode %v", got)
			}
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			if e := executeCommand(root, args); ExitCode(e) != 2 {
				t.Fatalf("expected usage error, got %v", e)
			}
		})
	}
}
func TestNativeRequestCannotPromptInNoInputMode(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if e := executeCommand(root, []string{"native", "setup", "--request-access", "--no-input"}); ExitCode(e) != 2 {
		t.Fatalf("expected rejection before helper launch, got %v", e)
	}
}
