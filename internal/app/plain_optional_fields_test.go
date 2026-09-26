package app

import (
	"bytes"
	"testing"

	"github.com/agis/acal/internal/backend"
)

func TestEventsUpdateDryRunPlainOptionalFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	for _, tc := range []struct {
		name   string
		args   []string
		fields string
		want   string
	}{
		{"title", []string{"--title", "Changed"}, "title", "Changed\n"},
		{"false", []string{"--all-day=false"}, "all_day", "false\n"},
		{"true", []string{"--all-day=true"}, "all_day", "true\n"},
		{"timestamp", []string{"--start", "2026-10-01T09:00:00Z"}, "start", "2026-10-01 09:00:00 +0000 UTC\n"},
		{"columns", []string{"--title", "Changed\tTitle\n"}, "notes,title,all_day,start,missing", "<nil>\tChanged\\tTitle\\n\t<nil>\t<nil>\t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			cmd := NewRootCommand()
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			args := []string{"events", "update", "review@1", "--dry-run", "--plain", "--fields", tc.fields, "--tz", "UTC"}
			cmd.SetArgs(append(args, tc.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("stdout = %q, want %q", got, tc.want)
			}
			if errOut.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errOut.String())
			}
		})
	}
}
