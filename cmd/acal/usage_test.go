package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestParserErrorOutputPreferences(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "acal")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, tc := range []struct {
		name                         string
		args                         []string
		env, user, project, explicit string
		structured                   bool
	}{
		{name: "environment", env: "json", structured: true},
		{name: "jsonl environment", env: "jsonl", structured: true},
		{name: "false flag", args: []string{"--json=false"}},
		{name: "false disables inherited", args: []string{"--json=false"}, env: "json"},
		{name: "plain overrides", args: []string{"--plain"}, env: "json"},
		{name: "last boolean wins", args: []string{"--json", "--json=false"}},
		{name: "repeated enabled", args: []string{"--json=false", "--json"}, structured: true},
		{name: "user config", user: "output = 'json'", structured: true},
		{name: "project config", user: "output = 'plain'", project: "output = 'json'", structured: true},
		{name: "explicit config", project: "output = 'plain'", explicit: "output = 'jsonl'", structured: true},
		{name: "environment overrides config", user: "output = 'json'", env: "plain"},
		{name: "profile", user: "output = 'plain'\n[profiles.agent]\noutput = 'json'", args: []string{"--profile", "agent"}, structured: true},
		{name: "invalid config", user: "broken toml!", env: "json", structured: true},
		{name: "invalid timeout keeps output", user: "output = 'json'\ntimeout = 'oops'", structured: true},
		{name: "literal flag value", args: []string{"events", "add", "--notes", "--json", "--unknown"}},
		{name: "terminator", args: []string{"events", "show", "--", "--json", "extra"}},
		{name: "mode before terminator", args: []string{"--json", "events", "show", "--", "one", "two"}, structured: true},
		{name: "unknown root command", args: []string{"bogus", "--json"}, structured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configDir := filepath.Join(dir, "acal")
			if err := os.Mkdir(configDir, 0700); err != nil {
				t.Fatal(err)
			}
			for path, contents := range map[string]string{filepath.Join(configDir, "config.toml"): tc.user, filepath.Join(dir, ".acal.toml"): tc.project, filepath.Join(dir, "explicit.toml"): tc.explicit} {
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{}, tc.args...)
			if tc.explicit != "" {
				args = append(args, "--config", filepath.Join(dir, "explicit.toml"))
			}
			if !slices.Contains(args, "events") && !slices.Contains(args, "bogus") {
				args = append(args, "events", "show")
			}
			cmd := exec.Command(binary, args...)
			cmd.Dir = dir
			var env []string
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "ACAL_") {
					env = append(env, value)
				}
			}
			cmd.Env = append(env, "HOME="+dir, "XDG_CONFIG_HOME="+dir, "ACAL_OUTPUT="+tc.env)
			var out, errOut bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errOut
			err := cmd.Run()
			if err == nil || cmd.ProcessState.ExitCode() != 2 || out.Len() != 0 {
				t.Fatalf("exit=%v stdout=%s stderr=%s", err, &out, &errOut)
			}
			if json.Valid(errOut.Bytes()) != tc.structured {
				t.Fatalf("structured=%v stderr=%s", tc.structured, &errOut)
			}
			if strings.Contains(errOut.String(), "parse config") || strings.Contains(errOut.String(), "invalid duration") {
				t.Fatalf("original parser error was replaced: %s", &errOut)
			}
		})
	}
}
