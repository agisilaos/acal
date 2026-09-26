package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsageExitContract(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "acal")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cases := [][]string{
		{"nonsense"},
		{"events", "nonsense"}, {"history", "nonsense"}, {"queries", "nonsense"}, {"view", "nonsense"}, {"calendars", "nonsense"},
		{"status", "nonsense"}, {"setup", "extra"}, {"doctor", "extra"}, {"version", "extra"},
		{"events", "add", "--calendar", "Work", "--title", "Preview", "--start", "2026-10-01T09:00Z", "--duration", "1h", "--dry-run", "extra"},
		{"queries", "list", "extra"}, {"history", "list", "extra"},
		{"events", "show"},
		{"events", "show", "one", "two"},
		{"history", "list", "--limit", "abc"},
		{"history", "list", "--unknown"},
		{"--unknown"},
		{"history", "list", "--timeout", "abc"},
		{"history", "list", "--limit"},
	}
	for _, args := range cases {
		for _, mode := range []string{"plain", "json", "jsonl"} {
			t.Run(strings.Join(args, " ")+"/"+mode, func(t *testing.T) {
				// Put the mode first so a missing flag value remains missing.
				argv := append([]string{"--" + mode}, args...)
				cmd := exec.Command(binary, argv...)
				cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+dir, "ACAL_CONFIG="+filepath.Join(dir, "missing.toml"))
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				if err == nil || cmd.ProcessState.ExitCode() != 2 {
					t.Fatalf("exit=%v stderr=%s", err, &stderr)
				}
				if stdout.Len() != 0 {
					t.Fatalf("unexpected stdout: %s", &stdout)
				}
				if mode == "plain" {
					if !strings.HasPrefix(stderr.String(), "error: ") {
						t.Fatalf("stderr=%s", &stderr)
					}
					return
				}
				var envelope struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Error.Code != "INVALID_USAGE" || envelope.Error.Message == "" {
					t.Fatalf("stderr=%s", &stderr)
				}
			})
		}
	}
	for _, args := range [][]string{{"events"}, {"queries"}, {"history"}, {"calendars"}, {"view"}, {"--help"}, {"help", "events"}, {"version"}, {"completion", "bash"}, {"__complete", "events", ""}} {
		cmd := exec.Command(binary, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}
