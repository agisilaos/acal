package app

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestQueriesSaveListDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"queries", "save", "next7", "--from", "today", "--to", "+7d", "--where", `title~standup`, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	cmd = NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"queries", "list", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list failed: %v", err)
	}

	cmd = NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"queries", "delete", "next7", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestQueriesRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{events: []contract.Event{
		{ID: "e1", Title: "Standup", Start: base, End: base.Add(30 * time.Minute)},
		{ID: "e2", Title: "Planning", Start: base.Add(24 * time.Hour), End: base.Add(25 * time.Hour)},
	}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"queries", "save", "standups", "--from", "2026-02-20", "--to", "2026-02-22", "--where", `title~standup`, "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	cmd = NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"queries", "run", "standups", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	var got struct {
		Data []contract.Event `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Data) != 1 || got.Data[0].ID != "e1" {
		t.Fatalf("unexpected query results: %+v", got.Data)
	}
}
