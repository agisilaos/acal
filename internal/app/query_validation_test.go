package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type predicateValidationBackend struct {
	strictNoCallBackend
	items []contract.Event
	err   error
	calls int
}

func (b *predicateValidationBackend) ListEvents(context.Context, backend.EventFilter) ([]contract.Event, error) {
	b.calls++
	return b.items, b.err
}

func TestQueryPredicateValidationBeforeListing(t *testing.T) {
	for _, saved := range []bool{false, true} {
		mode := "direct"
		if saved {
			mode = "saved"
		}
		for _, scenario := range []struct {
			name  string
			items []contract.Event
			err   error
		}{
			{name: "empty"},
			{name: "short-circuit", items: []contract.Event{{Title: "unmatched"}}},
			{name: "backend-failure", err: errors.New("listing unavailable")},
		} {
			for _, tc := range []struct {
				name   string
				wheres []string
				from   string
				want   string
			}{
				{"field", []string{"title==absent", "unknown==x"}, "2026-02-20", "unsupported field in --where: unknown"},
				{"string-operator", []string{"title==absent", "title>x"}, "2026-02-20", "operator > not supported for string fields"},
				{"time-value", []string{"title==absent", "start>=bad"}, "2026-02-20", `time predicate expects RFC3339 value, got "bad"`},
				{"time-operator", []string{"title==absent", "end~2026-02-20T10:00:00Z"}, "2026-02-20", "operator ~ not supported for time fields"},
				{"syntax-first", []string{"unknown==x", "badclause"}, "2026-02-20", "invalid where clause: badclause"},
				{"semantic-order", []string{"unknown==x", "start>=bad"}, "2026-02-20", "unsupported field in --where: unknown"},
				{"time-value-before-operator", []string{"start~bad"}, "2026-02-20", `time predicate expects RFC3339 value, got "bad"`},
				{"range-first", []string{"unknown==x"}, "invalid", "invalid --from:"},
			} {
				t.Run(mode+"/"+scenario.name+"/"+tc.name, func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					fb := &predicateValidationBackend{items: scenario.items, err: scenario.err}
					origFactory := backendFactory
					backendFactory = func(string) (backend.Backend, error) { return fb, nil }
					t.Cleanup(func() { backendFactory = origFactory })
					args := []string{"events", "query", "--from", tc.from, "--to", "2026-02-22"}
					for _, clause := range tc.wheres {
						args = append(args, "--where", clause)
					}
					if saved {
						args = append([]string{"queries", "save", "invalid"}, args[2:]...)
						cmd := NewRootCommand()
						cmd.SetOut(io.Discard)
						cmd.SetErr(io.Discard)
						cmd.SetArgs(args)
						if err := cmd.Execute(); err != nil {
							t.Fatalf("save must remain permissive: %v", err)
						}
						store, err := loadSavedQueries()
						if err != nil || !reflect.DeepEqual(store["invalid"].Wheres, tc.wheres) {
							t.Fatalf("raw saved predicates changed: %+v, %v", store, err)
						}
						args = []string{"queries", "run", "invalid"}
					}
					var stderr bytes.Buffer
					cmd := NewRootCommand()
					cmd.SetOut(io.Discard)
					cmd.SetErr(&stderr)
					cmd.SetArgs(append(args, "--json"))
					err := cmd.Execute()
					if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("got %v; want exit 2 and %q", err, tc.want)
					}
					if !strings.Contains(stderr.String(), string(contract.ErrInvalidUsage)) {
						t.Fatalf("missing structured invalid-usage error: %s", stderr.String())
					}
					if fb.calls != 0 {
						t.Fatalf("listing called %d times for invalid query", fb.calls)
					}
				})
			}
		}
	}
}

func TestCompiledQueryExecution(t *testing.T) {
	for _, mode := range []string{"direct", "saved"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			base := mustRFC3339(t, "2026-02-20T09:00:00Z")
			fb := &predicateValidationBackend{items: []contract.Event{
				{ID: "match", Title: "Standup", CalendarName: "Work", Start: base},
				{ID: "wrong-calendar", Title: "Standup", CalendarName: "Personal", Start: base},
				{ID: "wrong-title", Title: "Planning", CalendarName: "Work", Start: base},
			}}
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = origFactory })
			wheres := []string{" TiTLe ~ STAND ", "calendar_name==work", "start==2026-02-20T10:00:00+01:00", " "}
			args := []string{"events", "query", "--from", "2026-02-20", "--to", "2026-02-22"}
			for _, clause := range wheres {
				args = append(args, "--where", clause)
			}
			if mode == "saved" {
				if err := writeSavedQueries(map[string]savedQuery{"valid": {
					Name: "valid", From: "2026-02-20", To: "2026-02-22", Wheres: wheres,
				}}); err != nil {
					t.Fatal(err)
				}
				args = []string{"queries", "run", "valid"}
			}
			var stdout bytes.Buffer
			cmd := NewRootCommand()
			cmd.SetOut(&stdout)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append(args, "--json"))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Data []contract.Event `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if fb.calls != 1 || len(got.Data) != 1 || got.Data[0].ID != "match" {
				t.Fatalf("calls=%d results=%+v; want one call and matching event", fb.calls, got.Data)
			}
		})
	}
}
