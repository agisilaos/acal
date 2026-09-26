package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestHealthCommandOutcomes(t *testing.T) {
	commands := []struct {
		name    string
		message string
		hint    string
	}{
		{name: "doctor", message: "doctor checks not ready"},
		{name: "status", message: "status not ready", hint: "Run `acal setup` for remediation"},
		{name: "status explain", message: "status not ready"},
		{name: "setup", message: "setup not ready", hint: "Run `acal setup` again after applying next_steps"},
	}
	states := []struct {
		name     string
		checks   []contract.DoctorCheck
		ready    bool
		degraded bool
	}{
		{name: "healthy", ready: true, checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok"}, {Name: "calendar_access", Status: "ok"},
			{Name: "calendar_db", Status: "ok"}, {Name: "calendar_db_read", Status: "ok"},
		}},
		{name: "critical", checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok"}, {Name: "calendar_access", Status: "fail"},
		}},
		{name: "degraded", ready: true, degraded: true, checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok"}, {Name: "calendar_access", Status: "ok"},
			{Name: "calendar_db_read", Status: "fail"},
		}},
		{name: "missing required checks"},
		{name: "missing osascript", checks: []contract.DoctorCheck{{Name: "calendar_access", Status: "ok"}}},
		{name: "missing calendar access", checks: []contract.DoctorCheck{{Name: "osascript", Status: "ok"}}},
	}
	type healthState struct {
		Ready    bool `json:"ready"`
		Degraded bool `json:"degraded"`
	}
	origFactory, origArgs := backendFactory, os.Args
	t.Cleanup(func() { backendFactory, os.Args = origFactory, origArgs })
	doctorErr := errors.New("backend health check failed")
	for _, command := range commands {
		for _, mode := range []string{"plain", "json"} {
			for _, state := range states {
				for _, withError := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/backend_error=%t", command.name, mode, state.name, withError), func(t *testing.T) {
						fb := &adminBackend{checks: state.checks}
						if withError {
							fb.doctorErr = doctorErr
						}
						backendFactory = func(string) (backend.Backend, error) { return fb, nil }
						args := append(strings.Fields(command.name), "--"+mode)
						os.Args = append([]string{"acal"}, args...)
						cmd := NewRootCommand()
						var stdout, stderr bytes.Buffer
						cmd.SetOut(&stdout)
						cmd.SetErr(&stderr)
						cmd.SetArgs(args)
						err := cmd.Execute()
						if err != nil {
							renderTopLevelError(cmd, err)
						}
						wantCode := 0
						if !state.ready {
							wantCode = 6
						}
						if code := ExitCode(err); code != wantCode {
							t.Fatalf("exit code = %d, want %d (error: %v)", code, wantCode, err)
						}
						if !state.ready {
							var appErr AppError
							if !errors.As(err, &appErr) || appErr.Printed != withError {
								t.Fatalf("error = %#v, want AppError with Printed=%t", err, withError)
							}
							if withError && !errors.Is(appErr.Err, doctorErr) {
								t.Fatalf("backend error was not preserved: %v", appErr.Err)
							}
							if !withError && err.Error() != command.message {
								t.Fatalf("message = %q, want %q", err.Error(), command.message)
							}
						}

						wantState := healthState{Ready: state.ready, Degraded: state.degraded}
						if mode == "json" {
							var envelope struct {
								Command string          `json:"command"`
								Meta    healthState     `json:"meta"`
								Data    json.RawMessage `json:"data"`
							}
							decodeSingleHealthPayload(t, stdout.String(), &envelope)
							if envelope.Command != strings.ReplaceAll(command.name, " ", ".") {
								t.Fatalf("unexpected command in health report: %q", envelope.Command)
							}
							gotState := envelope.Meta
							if command.name == "status explain" {
								decodeSingleHealthPayload(t, string(envelope.Data), &gotState)
							}
							if gotState != wantState {
								t.Fatalf("health = %+v, want %+v", gotState, wantState)
							}
						} else if command.name == "setup" {
							var gotState healthState
							decodeSingleHealthPayload(t, stdout.String(), &gotState)
							if gotState != wantState {
								t.Fatalf("health = %+v, want %+v", gotState, wantState)
							}
						} else {
							prefix := fmt.Sprintf("ready=%t degraded=%t", state.ready, state.degraded)
							if !strings.HasPrefix(stdout.String(), prefix) || strings.Count(stdout.String(), "ready=") != 1 {
								t.Fatalf("expected one plain health report starting %q, got %q", prefix, stdout.String())
							}
						}

						wantStderr := !state.ready && (!withError || command.hint != "")
						if !wantStderr {
							if stderr.Len() != 0 {
								t.Fatalf("unexpected stderr: %q", stderr.String())
							}
							return
						}
						message, hint := command.message, ""
						if withError {
							message, hint = doctorErr.Error(), command.hint
						}
						if mode == "json" {
							var envelope contract.ErrorEnvelope
							decodeSingleHealthPayload(t, stderr.String(), &envelope)
							if envelope.Error.Code != contract.ErrBackendUnavailable || envelope.Error.Message != message || envelope.Error.Hint != hint {
								t.Fatalf("unexpected error report: %+v", envelope.Error)
							}
						} else {
							want := "error: " + message + "\n"
							if hint != "" {
								want += "hint: " + hint + "\n"
							}
							if stderr.String() != want {
								t.Fatalf("stderr = %q, want %q", stderr.String(), want)
							}
						}
					})
				}
			}
		}
	}
}

func decodeSingleHealthPayload(t *testing.T, text string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode health payload: %v; output: %q", err, text)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("expected exactly one JSON payload, got trailing output (error: %v): %q", err, text)
	}
}

type failingHealthWriter struct{}

func (failingHealthWriter) Write([]byte) (int, error) {
	return 0, errors.New("health output unavailable")
}

func TestHealthCommandsIgnoreRendererErrors(t *testing.T) {
	origFactory := backendFactory
	t.Cleanup(func() { backendFactory = origFactory })
	for _, command := range []string{"doctor", "status", "status explain", "setup"} {
		for _, mode := range []string{"plain", "json"} {
			for _, ready := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/ready=%t", command, mode, ready), func(t *testing.T) {
					fb := &adminBackend{doctorErr: errors.New("backend health check failed")}
					wantCode := 6
					if ready {
						fb.checks = []contract.DoctorCheck{
							{Name: "osascript", Status: "ok"}, {Name: "calendar_access", Status: "ok"},
						}
						wantCode = 0
					}
					backendFactory = func(string) (backend.Backend, error) { return fb, nil }
					cmd := NewRootCommand()
					cmd.SetOut(failingHealthWriter{})
					cmd.SetErr(failingHealthWriter{})
					cmd.SetArgs(append(strings.Fields(command), "--"+mode))
					err := cmd.Execute()
					if code := ExitCode(err); code != wantCode {
						t.Fatalf("exit code = %d, want %d (error: %v)", code, wantCode, err)
					}
					if !ready {
						var appErr AppError
						if !errors.As(err, &appErr) || !appErr.Printed || !errors.Is(appErr.Err, fb.doctorErr) {
							t.Fatalf("expected printed backend error despite writer failure, got %#v", err)
						}
					}
				})
			}
		}
	}
}
