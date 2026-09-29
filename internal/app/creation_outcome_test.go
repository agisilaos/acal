package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type creationFailureBackend struct {
	scopeCaptureBackend
	err          error
	created      []contract.Event
	afterCreate  func()
	waitForClose bool
	finished     chan struct{}
}

func (b *creationFailureBackend) AddEvent(ctx context.Context, in backend.EventCreateInput) (*contract.Event, error) {
	if b.finished != nil {
		defer close(b.finished)
	}
	b.addCalls++
	b.created = append(b.created, contract.Event{ID: "created-but-unreported", CalendarName: in.Calendar, Title: in.Title, Start: in.Start, End: in.End})
	if b.afterCreate != nil {
		b.afterCreate()
	}
	if b.waitForClose {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, b.err
}

// Control context completion after the fake mutation without a timer race.
type creationTestContext struct {
	context.Context
	done    chan struct{}
	failure error
}

func (c *creationTestContext) Done() <-chan struct{} { return c.done }
func (c *creationTestContext) Err() error {
	select {
	case <-c.done:
		return c.failure
	default:
		return nil
	}
}
func (c *creationTestContext) Deadline() (time.Time, bool) {
	return time.Date(2040, 1, 2, 9, 0, 0, 0, time.UTC), errors.Is(c.failure, context.DeadlineExceeded)
}

func TestCreationContextFailureAfterSideEffect(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx := &creationTestContext{Context: context.Background(), done: make(chan struct{}), failure: failure}
			be := &creationFailureBackend{afterCreate: func() { close(ctx.done) }, waitForClose: true, finished: make(chan struct{})}
			start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
			in := backend.EventCreateInput{Calendar: "Work", Title: "Review", Start: start, End: start.Add(30 * time.Minute)}
			item, err := addEventWithTimeout(ctx, be, in)
			<-be.finished
			if item != nil || !errors.Is(err, failure) || be.addCalls != 1 || len(be.created) != 1 {
				t.Fatalf("item=%v err=%v calls=%d created=%v", item, err, be.addCalls, be.created)
			}
			assertCreationUncertainty(t, backendErrorMeta(err), creationInspectionHint(err))
		})
	}
}

func TestCreationConsumersReportUnknownOutcome(t *testing.T) {
	for _, name := range []string{"quick-add", "events quick-add", "add", "copy", "batch", "import", "undo", "redo"} {
		for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
			t.Run(name+"/"+failure.Error(), func(t *testing.T) {
				setupReminderHistory(t)
				start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
				event := &contract.Event{ID: "source@123", CalendarName: "Work", Title: "Review", Start: start, End: start.Add(30 * time.Minute)}
				entry := historyEntry{Type: "add", EventID: event.ID, Created: event, Independent: true}
				if name == "undo" {
					entry = historyEntry{Type: "delete", EventID: event.ID, Deleted: event, Independent: true}
				}
				if err := writeHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				if err := writeRedoHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(historyFilePath())
				if err != nil {
					t.Fatal(err)
				}
				redoBefore, err := os.ReadFile(redoFilePath())
				if err != nil {
					t.Fatal(err)
				}
				be := &creationFailureBackend{scopeCaptureBackend: scopeCaptureBackend{getEvent: event}, err: failure}
				out, errOut, cmdErr := runCreationCommand(t, be, append(creationCommandArgs(t, name), "--json", "--tz", "UTC")...)
				wantExit := 6
				if name == "quick-add" || name == "events quick-add" || name == "batch" {
					wantExit = 1
				}
				if ExitCode(cmdErr) != wantExit || be.addCalls != 1 || len(be.created) != 1 {
					t.Fatalf("exit=%d want=%d calls=%d created=%v stdout=%s stderr=%s", ExitCode(cmdErr), wantExit, be.addCalls, be.created, out, errOut)
				}
				var meta map[string]any
				var hint string
				if name == "batch" {
					if errOut != "" {
						t.Fatalf("unexpected batch stderr: %s", errOut)
					}
					var env struct {
						SchemaVersion string `json:"schema_version"`
						Data          []struct {
							OK   bool           `json:"ok"`
							Meta map[string]any `json:"meta"`
							Hint string         `json:"hint"`
						} `json:"data"`
					}
					if err := json.Unmarshal([]byte(out), &env); err != nil || env.SchemaVersion != "v1" || len(env.Data) != 1 || env.Data[0].OK {
						t.Fatalf("invalid batch error result: %s (%v)", out, err)
					}
					meta, hint = env.Data[0].Meta, env.Data[0].Hint
				} else {
					if out != "" {
						t.Fatalf("failure wrote stdout: %s", out)
					}
					var env contract.ErrorEnvelope
					if err := json.Unmarshal([]byte(errOut), &env); err != nil || env.SchemaVersion != "v1" {
						t.Fatalf("invalid error envelope: %s (%v)", errOut, err)
					}
					wantCode := contract.ErrBackendUnavailable
					if name == "quick-add" || name == "events quick-add" {
						wantCode = contract.ErrGeneric
					}
					if env.Error.Code != wantCode {
						t.Fatalf("code=%s want=%s", env.Error.Code, wantCode)
					}
					meta, hint = env.Meta, env.Error.Hint
					if name == "import" && (meta["count"] != float64(0) || meta["failed_item"] != float64(1)) {
						t.Fatalf("import lost progress: %v", meta)
					}
				}
				assertCreationUncertainty(t, meta, hint)
				kind := "timeout"
				if failure == context.Canceled {
					kind = "canceled"
				}
				if meta["kind"] != kind {
					t.Fatalf("kind=%v want=%s", meta["kind"], kind)
				}
				after, err := os.ReadFile(historyFilePath())
				if err != nil {
					t.Fatal(err)
				}
				redoAfter, err := os.ReadFile(redoFilePath())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) || !bytes.Equal(redoBefore, redoAfter) {
					t.Fatal("uncertain creation changed history or redo")
				}
			})
		}
	}
}

func assertCreationUncertainty(t *testing.T, meta map[string]any, hint string) {
	t.Helper()
	for key, want := range map[string]any{"phase": "backend.add_event", "outcome": "unknown", "verified": false, "calendar": "Work", "title": "Review", "start": "2026-10-01T09:00:00Z", "end": "2026-10-01T09:30:00Z"} {
		if meta[key] != want {
			t.Fatalf("meta[%s]=%v want=%v; all=%v", key, meta[key], want, meta)
		}
	}
	if _, present := meta["applied"]; present {
		t.Fatalf("uncertain creation asserted applied: %v", meta)
	}
	for _, want := range []string{"Inspect Calendar before retrying", "may already have been created", `calendar "Work"`, `title "Review"`, "2026-10-01T09:00:00Z", "2026-10-01T09:30:00Z"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("missing %q in hint %q", want, hint)
		}
	}
	if strings.Contains(hint, "Retry with") || strings.Contains(hint, "Retry command") {
		t.Fatalf("unsafe retry hint: %s", hint)
	}
}

func creationCommandArgs(t *testing.T, name string) []string {
	t.Helper()
	switch name {
	case "quick-add", "events quick-add":
		return append(strings.Fields(name), "2026-10-01 09:00 Review @Work 30m")
	case "add":
		return []string{"events", "add", "--calendar", "Work", "--title", "Review", "--start", "2026-10-01T09:00:00Z", "--duration", "30m"}
	case "copy":
		return []string{"events", "copy", "source@123", "--to", "2026-10-01T09:00:00Z", "--duration", "30m"}
	case "batch":
		path := filepath.Join(t.TempDir(), "create.jsonl")
		if err := os.WriteFile(path, []byte(`{"op":"add","calendar":"Work","title":"Review","start":"2026-10-01T09:00:00Z","duration":"30m"}`), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{"events", "batch", "--file", path}
	case "import":
		path := filepath.Join(t.TempDir(), "create.ics")
		if err := os.WriteFile(path, []byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Review\nDTSTART:20261001T090000Z\nDTEND:20261001T093000Z\nEND:VEVENT\nEND:VCALENDAR\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{"events", "import", "--file", path, "--calendar", "Work"}
	default:
		return []string{"history", name}
	}
}

func runCreationCommand(t *testing.T, be backend.Backend, args ...string) (string, string, error) {
	t.Helper()
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return be, nil }
	defer func() { backendFactory = original }()
	cmd := NewRootCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestCreationOrdinaryErrorsAndRejections(t *testing.T) {
	for _, name := range []string{"quick-add", "events quick-add", "add"} {
		for _, tc := range []struct {
			name string
			err  error
			code contract.ErrorCode
			exit int
		}{
			{"ordinary", errors.New("calendar not found"), contract.ErrGeneric, 1},
			{"recurring", &backend.WriteRejectedError{Reason: "recurring"}, contract.ErrUnsupported, 2},
			{"permission", &backend.WriteRejectedError{Reason: "permission"}, contract.ErrPermissionDenied, 6},
		} {
			if name != "add" && tc.name != "ordinary" {
				continue
			}
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				be := &scopeCaptureBackend{addErr: tc.err}
				out, errOut, err := runCreationCommand(t, be, append(creationCommandArgs(t, name), "--json", "--tz", "UTC")...)
				var env contract.ErrorEnvelope
				if parseErr := json.Unmarshal([]byte(errOut), &env); parseErr != nil || out != "" || ExitCode(err) != tc.exit || env.Error.Code != tc.code {
					t.Fatalf("err=%v parse=%v envelope=%+v stdout=%s", err, parseErr, env, out)
				}
				if env.Meta["outcome"] != nil || env.Meta["verified"] != nil || strings.Contains(env.Error.Hint, "may already have been created") {
					t.Fatalf("ordinary failure incorrectly marked uncertain: %+v", env)
				}
				if tc.name != "ordinary" && (env.Meta["kind"] != "write_rejected" || env.Meta["applied"] != false) {
					t.Fatalf("pre-write rejection lost classification: %+v", env)
				}
			})
		}
	}
}

func TestCreationFailureJSONLAndPlain(t *testing.T) {
	for _, name := range []string{"quick-add", "events quick-add", "add"} {
		for _, mode := range []string{"--jsonl", "--plain"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				be := &creationFailureBackend{err: context.DeadlineExceeded}
				out, errOut, err := runCreationCommand(t, be, append(creationCommandArgs(t, name), mode, "--tz", "UTC")...)
				if err == nil || out != "" || !strings.Contains(errOut, "Inspect Calendar before retrying") {
					t.Fatalf("err=%v stdout=%s stderr=%s", err, out, errOut)
				}
				if mode == "--jsonl" {
					var env contract.ErrorEnvelope
					if err := json.Unmarshal([]byte(errOut), &env); err != nil {
						t.Fatal(err)
					}
					assertCreationUncertainty(t, env.Meta, env.Error.Hint)
				}
			})
		}
	}
}
