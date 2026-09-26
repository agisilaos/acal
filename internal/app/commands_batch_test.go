package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestExecuteBatchLineAddDryRun(t *testing.T) {
	title := "Standup"
	start := "2026-02-20T09:00"
	dur := "30m"
	res, err := executeBatchLine(context.Background(), &scopeCaptureBackend{}, batchLine{Op: "add", Calendar: "Work", Title: &title, Start: &start, Duration: &dur}, time.UTC, true)
	if err != nil {
		t.Fatalf("executeBatchLine failed: %v", err)
	}
	if res.View["op"] != "add" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestEventsBatchDryRun(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	content := "{\"op\":\"add\",\"calendar\":\"Work\",\"title\":\"Plan\",\"start\":\"2026-02-20T09:00\",\"duration\":\"30m\"}\n" +
		"{\"op\":\"delete\",\"id\":\"evt@792417600\"}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--dry-run", "--tz", "UTC", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 0 {
		t.Fatalf("expected exit code 0, got %d err=%v", code, err)
	}
	if fb.addCalls != 0 || fb.deleteCalls != 0 || fb.updateCalls != 0 {
		t.Fatalf("expected no backend write calls in dry-run")
	}
}

func TestEventsBatchMalformedJSONL(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	if err := os.WriteFile(f, []byte("{bad json}\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--dry-run", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 1 {
		t.Fatalf("expected exit code 1, got %d err=%v", code, err)
	}
}

func TestEventsBatchStrictFailsFast(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	content := "{bad json}\n" +
		"{\"op\":\"add\",\"calendar\":\"Work\",\"title\":\"Plan\",\"start\":\"2026-02-20T09:00\",\"duration\":\"30m\"}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--strict", "--dry-run", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 1 {
		t.Fatalf("expected exit code 1, got %d err=%v", code, err)
	}
}

func TestEventsBatchIncludesOpID(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	content := "{\"op\":\"add\",\"calendar\":\"Work\",\"title\":\"Plan\",\"start\":\"2026-02-20T09:00\",\"duration\":\"30m\"}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var got struct {
		Meta map[string]any   `json:"meta"`
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one row, got %d", len(got.Data))
	}
	if _, ok := got.Data[0]["op_id"]; !ok {
		t.Fatalf("expected op_id in row: %+v", got.Data[0])
	}
	if _, ok := got.Data[0]["tx_id"]; !ok {
		t.Fatalf("expected tx_id in row: %+v", got.Data[0])
	}
	if _, ok := got.Meta["tx_id"]; !ok {
		t.Fatalf("expected tx_id in meta: %+v", got.Meta)
	}
}

func TestEventsBatchWritesHistoryWithTxAndOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{
		getEvent: &contract.Event{
			ID:       "evt@792417600",
			Title:    "Standup",
			Start:    base,
			End:      base.Add(30 * time.Minute),
			Sequence: 1,
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	content := "{\"op\":\"update\",\"id\":\"evt@792417600\",\"title\":\"Revised\"}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	entries, err := readHistory()
	if err != nil {
		t.Fatalf("readHistory failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one history entry, got %d", len(entries))
	}
	if entries[0].TxID == "" || entries[0].OpID == "" {
		t.Fatalf("expected tx/op identifiers in history: %+v", entries[0])
	}
}

func TestEventsBatchRejectsUnknownFields(t *testing.T) {
	for _, row := range []string{
		`{"op":"add","calendar":"Work","title":"Batch","start":"2026-10-01T09:00","duration":"30m","repeat":"daily*5"}`,
		`{"op":"update","id":"fixture","title":"Changed","repeat":"daily*5"}`,
		`{"op":"delete","id":"fixture","repeat":null}`,
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dryRun=%t", row, dryRun), func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				fb := &scopeCaptureBackend{}
				origFactory := backendFactory
				backendFactory = func(string) (backend.Backend, error) { return fb, nil }
				t.Cleanup(func() { backendFactory = origFactory })
				path := filepath.Join(t.TempDir(), "ops.jsonl")
				if err := os.WriteFile(path, []byte(row), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := NewRootCommand()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(io.Discard)
				args := []string{"events", "batch", "--file", path, "--json", "--strict"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
				if code := ExitCode(cmd.Execute()); code != 1 {
					t.Fatalf("exit = %d; output: %s", code, &out)
				}
				if !strings.Contains(out.String(), `unknown field \"repeat\"`) {
					t.Fatalf("missing field diagnostic: %s", &out)
				}
				if fb.addCalls+fb.updateCalls+fb.deleteCalls != 0 {
					t.Fatal("invalid row reached backend write")
				}
			})
		}
	}
}

func TestEventsBatchSchemaErrorContinuation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		flags        []string
		rows, writes int
	}{
		{"default continues", nil, 3, 2},
		{"explicit continues", []string{"--continue-on-error"}, 3, 2},
		{"strict overrides continue", []string{"--strict", "--continue-on-error"}, 2, 1},
		{"continue disabled", []string{"--continue-on-error=false"}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			fb := &scopeCaptureBackend{}
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = origFactory })
			valid := `{"op":"add","calendar":"Work","title":"Batch","start":"2026-10-01T09:00","duration":"30m"}`
			invalid := strings.TrimSuffix(valid, "}") + `,"metadata":{"source":"fixture"}}`
			path := filepath.Join(t.TempDir(), "ops.jsonl")
			if err := os.WriteFile(path, []byte(valid+"\n\n"+invalid+"\n"+valid+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append([]string{"events", "batch", "--file", path, "--json"}, tc.flags...))
			if code := ExitCode(cmd.Execute()); code != 1 {
				t.Fatalf("exit = %d; output: %s", code, &out)
			}
			var got struct {
				Data []struct {
					Line  int
					OK    bool
					Error string
					OpID  string `json:"op_id"`
				} `json:"data"`
				Meta struct{ Count, Errors int } `json:"meta"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Data) != tc.rows || got.Meta.Count != tc.rows || got.Meta.Errors != 1 {
				t.Fatalf("unexpected response: %s", &out)
			}
			failed := got.Data[1]
			if failed.Line != 3 || failed.OK || failed.Error != `json: unknown field "metadata"` || failed.OpID != "op-0003-parse" {
				t.Fatalf("unexpected error row: %+v", failed)
			}
			if !got.Data[0].OK || (tc.rows == 3 && !got.Data[2].OK) {
				t.Fatalf("valid row failed: %s", &out)
			}
			if fb.addCalls != tc.writes {
				t.Fatalf("writes = %d, want %d", fb.addCalls, tc.writes)
			}
			history, err := readHistory()
			if err != nil || len(history) != tc.writes {
				t.Fatalf("history count = %d, err = %v", len(history), err)
			}
		})
	}
}

func TestDecodeBatchLine(t *testing.T) {
	for _, tc := range []struct{ name, line, wantError string }{
		{"malformed", `{bad json}`, "invalid json"},
		{"trailing object", `{"op":"delete","id":"fixture"} {"repeat":"daily*5"}`, "invalid json"},
		{"trailing garbage", `{"op":"delete","id":"fixture"} garbage`, "invalid json"},
		{"typo", `{"op":"add","duraton":"30m"}`, `json: unknown field "duraton"`},
		{"wrong type", `{"op":"add","all_day":"true"}`, "cannot unmarshal string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeBatchLine(tc.line)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %s", err, tc.wantError)
			}
		})
	}
	row, err := decodeBatchLine(`{"op":"update","id":"fixture","calendar":"Work","title":"Title","start":"2026-10-01T09:00","end":"2026-10-01T10:00","duration":"30m","location":"Room","notes":"Notes","url":"https://example.com","all_day":false,"scope":"this"}`)
	if err != nil {
		t.Fatal(err)
	}
	if row.Op != "update" || row.ID != "fixture" || row.Calendar != "Work" || row.Title == nil || *row.Title != "Title" || row.Start == nil || row.End == nil || row.Duration == nil || row.Location == nil || row.Notes == nil || row.URL == nil || row.AllDay == nil || *row.AllDay || row.Scope != "this" {
		t.Fatalf("supported fields lost: %+v", row)
	}
}
