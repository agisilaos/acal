package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

// Reads return the state at the time of the call, so a post-write read cannot
// masquerade as the original event when history is recorded.
type snapshotBackend struct {
	scopeCaptureBackend
	event contract.Event
	calls []string
}

func (b *snapshotBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	b.calls = append(b.calls, "read")
	if b.getErr != nil {
		return nil, b.getErr
	}
	event := b.event
	return &event, nil
}

func (b *snapshotBackend) UpdateEvent(_ context.Context, _ string, in backend.EventUpdateInput) (*contract.Event, error) {
	b.calls = append(b.calls, "write")
	b.updateInput = in
	if b.updateErr != nil {
		return nil, b.updateErr
	}
	if in.Title != nil {
		b.event.Title = *in.Title
	}
	if in.Start != nil {
		b.event.Start = *in.Start
	}
	if in.End != nil {
		b.event.End = *in.End
	}
	event := b.event
	return &event, nil
}

func newSnapshotBackend(t *testing.T) *snapshotBackend {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	start := time.Date(2020, 2, 20, 9, 0, 0, 0, time.UTC)
	return &snapshotBackend{event: contract.Event{ID: "event-1", Title: "Original", Start: start, End: start.Add(time.Hour), Sequence: 3}}
}

func snapshotString(s string) *string { return &s }

func runSnapshotUpdate(t *testing.T, b *snapshotBackend, mode string, row batchLine, dryRun bool, sequence string) (backend.EventUpdateInput, string, error) {
	t.Helper()
	originalFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return b, nil }
	defer func() { backendFactory = originalFactory }()
	row.Op, row.ID = "update", b.event.ID
	args := []string{"events", "update", row.ID}
	if mode == "batch" {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "ops.jsonl")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		args = []string{"events", "batch", "--file", path}
	} else {
		for _, flag := range []struct {
			name  string
			value *string
		}{{"title", row.Title}, {"start", row.Start}, {"end", row.End}, {"duration", row.Duration}} {
			if flag.value != nil {
				args = append(args, "--"+flag.name, *flag.value)
			}
		}
		if sequence != "" {
			args = append(args, "--if-match-seq", sequence)
		}
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--json", "--tz", "UTC")
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err != nil || !dryRun {
		return b.updateInput, out.String(), err
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var patch backend.EventUpdateInput
	if mode == "batch" {
		var rows []struct {
			Input backend.EventUpdateInput `json:"input"`
		}
		if err := json.Unmarshal(envelope.Data, &rows); err != nil || len(rows) != 1 {
			t.Fatalf("invalid batch preview: %s, %v", out.String(), err)
		}
		patch = rows[0].Input
	} else if err := json.Unmarshal(envelope.Data, &patch); err != nil {
		t.Fatal(err)
	}
	return patch, out.String(), nil
}

func assertSnapshotCalls(t *testing.T, b *snapshotBackend, want ...string) {
	t.Helper()
	if strings.Join(b.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", b.calls, want)
	}
}

func assertNoUpdateHistory(t *testing.T) {
	t.Helper()
	entries, err := readHistory()
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected history: %+v, %v", entries, err)
	}
}

func TestUpdateSnapshotHistoryAndUndo(t *testing.T) {
	for _, mode := range []string{"ordinary", "batch"} {
		t.Run(mode, func(t *testing.T) {
			b := newSnapshotBackend(t)
			before := b.event
			_, _, err := runSnapshotUpdate(t, b, mode, batchLine{Title: snapshotString("Changed")}, false, "")
			if err != nil {
				t.Fatal(err)
			}
			assertSnapshotCalls(t, b, "read", "write")
			entries, err := readHistory()
			if err != nil || len(entries) != 1 {
				t.Fatalf("history = %+v, %v", entries, err)
			}
			if !reflect.DeepEqual(entries[0].Prev, &before) || !reflect.DeepEqual(entries[0].Next, &b.event) {
				t.Fatalf("incorrect before/after snapshots: %+v", entries[0])
			}
			if _, _, err := undoLastHistory(context.Background(), b, false); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(b.event, before) {
				t.Fatalf("undo restored %+v, want %+v", b.event, before)
			}
			assertSnapshotCalls(t, b, "read", "write", "write")
		})
	}
}

func TestUpdateSnapshotTiming(t *testing.T) {
	for _, mode := range []string{"ordinary", "batch"} {
		for _, dryRun := range []bool{false, true} {
			for _, suppliedStart := range []bool{false, true} {
				name := mode
				if dryRun {
					name += "/preview"
				} else {
					name += "/write"
				}
				if suppliedStart {
					name += "/new-start"
				} else {
					name += "/existing-start"
				}
				t.Run(name, func(t *testing.T) {
					b := newSnapshotBackend(t)
					before := b.event
					row := batchLine{Duration: snapshotString("45m")}
					start := before.Start
					if suppliedStart {
						start = start.Add(24 * time.Hour)
						row.Start = snapshotString(start.Format(time.RFC3339))
					}
					patch, _, err := runSnapshotUpdate(t, b, mode, row, dryRun, "")
					if err != nil {
						t.Fatal(err)
					}
					if patch.End == nil || !patch.End.Equal(start.Add(45*time.Minute)) {
						t.Fatalf("end = %v, want %v", patch.End, start.Add(45*time.Minute))
					}
					if dryRun {
						if suppliedStart {
							assertSnapshotCalls(t, b)
						} else {
							assertSnapshotCalls(t, b, "read")
						}
						assertNoUpdateHistory(t)
					} else {
						assertSnapshotCalls(t, b, "read", "write")
						entries, err := readHistory()
						if err != nil || len(entries) != 1 || !reflect.DeepEqual(entries[0].Prev, &before) {
							t.Fatalf("incorrect timing history: %+v, %v", entries, err)
						}
					}
				})
			}
		}
	}
}

func TestUpdateSnapshotFailuresAndPreviews(t *testing.T) {
	for _, mode := range []string{"ordinary", "batch"} {
		for _, tc := range []struct {
			name       string
			row        batchLine
			dryRun     bool
			readFails  bool
			writeFails bool
			wantError  string
			wantCalls  string
		}{
			{name: "field preview", row: batchLine{Title: snapshotString("Changed")}, dryRun: true, readFails: true},
			{name: "write failure", row: batchLine{Title: snapshotString("Changed")}, writeFails: true, wantError: "write failed", wantCalls: "read,write"},
			{name: "duration read failure", row: batchLine{Duration: snapshotString("45m")}, readFails: true, wantError: "read failed", wantCalls: "read"},
			{name: "duration preview read failure", row: batchLine{Duration: snapshotString("45m")}, dryRun: true, readFails: true, wantError: "read failed", wantCalls: "read"},
			{name: "end read failure", row: batchLine{End: snapshotString("2020-02-20T11:00:00Z")}, readFails: true, wantError: "read failed", wantCalls: "read"},
			{name: "invalid duration first", row: batchLine{Duration: snapshotString("bad")}, readFails: true, wantError: "duration"},
			{name: "nonpositive duration first", row: batchLine{Duration: snapshotString("0m")}, readFails: true, wantError: "duration"},
			{name: "invalid end first", row: batchLine{End: snapshotString("bad")}, readFails: true, wantError: "datetime"},
			{name: "supplied end ordering first", row: batchLine{Start: snapshotString("2020-02-20T12:00:00Z"), End: snapshotString("2020-02-20T11:00:00Z")}, readFails: true, wantError: "after"},
			{name: "existing end ordering", row: batchLine{End: snapshotString("2020-02-20T08:00:00Z")}, wantError: "after", wantCalls: "read"},
			{name: "existing end preview", row: batchLine{End: snapshotString("2020-02-20T11:00:00Z")}, dryRun: true, wantCalls: "read"},
			{name: "supplied end preview", row: batchLine{Start: snapshotString("2020-02-20T09:00:00Z"), End: snapshotString("2020-02-20T11:00:00Z")}, dryRun: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				b := newSnapshotBackend(t)
				if tc.readFails {
					b.getErr = errors.New("read failed")
				}
				if tc.writeFails {
					b.updateErr = errors.New("write failed")
				}
				patch, output, err := runSnapshotUpdate(t, b, mode, tc.row, tc.dryRun, "")
				wantError := tc.wantError
				if mode == "batch" && tc.name == "invalid end first" {
					wantError = "invalid end"
				}
				if (err != nil) != (wantError != "") || !strings.Contains(output, wantError) {
					t.Fatalf("error = %v, output = %s, want %q", err, output, wantError)
				}
				if tc.dryRun && tc.row.End != nil && err == nil {
					want, _ := time.Parse(time.RFC3339, *tc.row.End)
					if patch.End == nil || !patch.End.Equal(want) {
						t.Fatalf("preview end = %v, want %v", patch.End, want)
					}
				}
				assertSnapshotCalls(t, b, strings.Split(tc.wantCalls, ",")...)
				assertNoUpdateHistory(t)
			})
		}
		t.Run(mode+"/optional snapshot failure", func(t *testing.T) {
			b := newSnapshotBackend(t)
			b.getErr = errors.New("read failed")
			_, _, err := runSnapshotUpdate(t, b, mode, batchLine{Title: snapshotString("Changed")}, false, "")
			if mode == "ordinary" {
				if err != nil {
					t.Fatal(err)
				}
				assertSnapshotCalls(t, b, "read", "write")
			} else {
				if err == nil {
					t.Fatal("expected snapshot failure")
				}
				assertSnapshotCalls(t, b, "read")
			}
			assertNoUpdateHistory(t)
		})
	}
}

func TestUpdateSnapshotSequenceAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		row       batchLine
		sequence  string
		dryRun    bool
		readFails bool
		wantCode  int
		wantCalls string
	}{
		{name: "shared snapshot", row: batchLine{Duration: snapshotString("45m")}, sequence: "3", wantCalls: "read,write"},
		{name: "preview sequence", row: batchLine{Title: snapshotString("Changed")}, sequence: "3", dryRun: true, wantCalls: "read"},
		{name: "mismatch", sequence: "2", wantCode: 7, wantCalls: "read"},
		{name: "unavailable", sequence: "3", readFails: true, wantCode: 4, wantCalls: "read"},
		{name: "invalid before sequence", row: batchLine{Duration: snapshotString("bad")}, sequence: "3", readFails: true, wantCode: 2},
		{name: "end and duration", row: batchLine{End: snapshotString("2020-02-20T11:00:00Z"), Duration: snapshotString("45m")}, sequence: "3", wantCode: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newSnapshotBackend(t)
			if tc.readFails {
				b.getErr = errors.New("read failed")
			}
			_, _, err := runSnapshotUpdate(t, b, "ordinary", tc.row, tc.dryRun, tc.sequence)
			if ExitCode(err) != tc.wantCode {
				t.Fatalf("exit = %d, want %d: %v", ExitCode(err), tc.wantCode, err)
			}
			assertSnapshotCalls(t, b, strings.Split(tc.wantCalls, ",")...)
			if tc.wantCode != 0 || tc.dryRun {
				assertNoUpdateHistory(t)
			}
		})
	}
	t.Run("batch end takes precedence", func(t *testing.T) {
		b := newSnapshotBackend(t)
		row := batchLine{End: snapshotString("2020-02-20T11:00:00Z"), Duration: snapshotString("invalid but ignored")}
		patch, _, err := runSnapshotUpdate(t, b, "batch", row, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if patch.End == nil || !patch.End.Equal(b.event.Start.Add(2*time.Hour)) {
			t.Fatalf("end = %v", patch.End)
		}
		assertSnapshotCalls(t, b, "read", "write")
	})
}
