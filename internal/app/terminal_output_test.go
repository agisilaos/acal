package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

const terminalPayload = "Café\x1b[2J\x1b]52;c;payload\a\u009b2J"

func assertDisplaySafe(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		if (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
			t.Fatalf("literal control %U in %q", r, text)
		}
	}
}

func TestProjectConfigErrorEscapesTerminalControls(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_BACKEND", "")
	if err := os.WriteFile(".acal.toml", []byte(`backend = "\u001b[2J\u001b]52;c;payload\u0007"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand()
	var stderr bytes.Buffer
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"queries", "list", "--plain", "--no-color"})
	if err := cmd.Execute(); err == nil || ExitCode(err) != 2 {
		t.Fatalf("expected invalid backend: %v", err)
	}
	assertDisplaySafe(t, stderr.String())
	if !strings.Contains(stderr.String(), `unknown backend: \u001b[2J`) {
		t.Fatalf("unexpected error: %q", stderr.String())
	}
}

func TestCustomDisplaysEscapeTerminalControls(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) {
		return &adminBackend{checks: []contract.DoctorCheck{{Name: terminalPayload, Status: terminalPayload, Message: terminalPayload}}}, nil
	}
	t.Cleanup(func() { backendFactory = original })
	entry := historyEntry{At: time.Now(), Type: terminalPayload, EventID: terminalPayload + "\t\n", TxID: terminalPayload, OpID: terminalPayload}
	if err := appendHistory(entry); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"doctor"}, {"status"}, {"status", "explain"}, {"history", "list"}, {"queries", "list", "--verbose", "--profile", terminalPayload}} {
		cmd := NewRootCommand()
		var out, stderr bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append(args, "--plain", "--no-color"))
		_ = cmd.Execute() // A failed health report still needs safe rendering.
		assertDisplaySafe(t, out.String()+stderr.String())
		if !strings.Contains(strings.ToLower(out.String()+stderr.String()), `café\u001b`) {
			t.Fatalf("%v did not render the supplied data: %q %q", args, out.String(), stderr.String())
		}
		if args[0] == "history" && (strings.Count(out.String(), "\t") != 4 || strings.Count(out.String(), "\n") != 1) {
			t.Fatalf("history row separators changed: %q", out.String())
		}
	}
}

func TestICSControlHandlingByDestination(t *testing.T) {
	event := contract.Event{ID: "event", Title: terminalPayload, Start: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)}
	raw := buildICS([]contract.Event{event}, time.UTC)
	display := terminalICS(raw)
	assertDisplaySafe(t, display)
	if !strings.Contains(display, `SUMMARY:Café\u001b[2J`) || strings.Count(display, "\r\n") != strings.Count(raw, "\r\n") {
		t.Fatalf("terminal export changed record structure: %q", display)
	}
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) {
		return &scopeCaptureBackend{events: []contract.Event{event}}, nil
	}
	t.Cleanup(func() { backendFactory = original })
	for _, dest := range []string{"redirected", "file", "json"} {
		t.Run(dest, func(t *testing.T) {
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			args := []string{"events", "export", "--from", "2026-10-01", "--to", "2026-10-02", "--tz", "UTC"}
			path := filepath.Join(t.TempDir(), "events.ics")
			if dest == "json" {
				args = append(args, "--json")
			} else {
				args = append(args, "--plain")
				if dest == "file" {
					args = append(args, "--out", path)
				}
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if dest == "file" {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				got = string(b)
			} else if dest == "json" {
				var env struct {
					Data struct {
						ICS string `json:"ics"`
					} `json:"data"`
				}
				if err := json.Unmarshal(out.Bytes(), &env); err != nil {
					t.Fatal(err)
				}
				got = env.Data.ICS
			}
			if !strings.Contains(got, "SUMMARY:"+escapeICSText(terminalPayload)+"\r\n") {
				t.Fatalf("serialized data changed for %s: %q", dest, got)
			}
		})
	}
}
