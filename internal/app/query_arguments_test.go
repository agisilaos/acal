package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/agis/acal/internal/contract"
)

func TestQueryLiteralWhereArguments(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, clause := range []string{`title~a,b`, `title~"a,b"`, `title~say "hello", friend`, `title~a,b,calendar==Work`} {
			for _, empty := range []bool{false, true} {
				t.Run(strings.Join([]string{clause, map[bool]string{false: "direct", true: "saved"}[saved], map[bool]string{false: "matches", true: "empty"}[empty]}, "/"), func(t *testing.T) {
					b := &limitAwareQueryBackend{scopeCaptureBackend: scopeCaptureBackend{events: []contract.Event{
						{ID: "match", Title: `a,b say "hello", friend`, CalendarName: "Work"},
						{ID: "excluded", Title: `a,b say "hello", friend`, CalendarName: "Personal"},
					}}}
					if empty {
						b.events = nil
					}
					installQueryBackend(t, b)
					wheres := []string{clause, "calendar==Work"}
					args := []string{"events", "query", "--from", "2026-02-20", "--to", "2026-02-22"}
					if saved {
						args = append([]string{"queries", "save", "literal"}, args[2:]...)
					}
					for _, w := range wheres {
						args = append(args, "--where", w)
					}
					cmd := NewRootCommand()
					var out bytes.Buffer
					cmd.SetOut(&out)
					cmd.SetErr(&out)
					cmd.SetArgs(append(args, "--json"))
					err := cmd.Execute()
					if saved {
						if err != nil {
							t.Fatalf("save: %v: %s", err, &out)
						}
						store, loadErr := loadSavedQueries()
						if loadErr != nil || !reflect.DeepEqual(store["literal"].Wheres, wheres) {
							t.Fatalf("literal arguments not preserved: %+v, %v", store, loadErr)
						}
						if b.calls != 0 {
							t.Fatal("save fetched events")
						}
						out.Reset()
						cmd = NewRootCommand()
						cmd.SetOut(&out)
						cmd.SetErr(&out)
						cmd.SetArgs([]string{"queries", "run", "literal", "--json"})
						err = cmd.Execute()
					}
					// Legacy comma-list syntax stays one clause; do not guess two predicates.
					if clause == `title~a,b,calendar==Work` {
						if err == nil || ExitCode(err) != 2 || b.calls != 0 {
							t.Fatalf("expected invalid single clause before fetch: %v: %s", err, &out)
						}
						return
					}
					if err != nil {
						t.Fatalf("query: %v: %s", err, &out)
					}
					var got struct {
						Data []contract.Event `json:"data"`
					}
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if b.calls != 1 || got.Data == nil {
						t.Fatalf("calls=%d output=%s", b.calls, &out)
					}
					if empty {
						if len(got.Data) != 0 {
							t.Fatalf("want empty: %s", &out)
						}
					} else if len(got.Data) != 1 || got.Data[0].ID != "match" {
						t.Fatalf("want only match: %s", &out)
					}
				})
			}
		}
	}
}

func TestQuerySortValidationBeforeFetch(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, tc := range []struct{ sort, order, want string }{
			{"garbage", "asc", "unsupported --sort"}, {"start", "sideways", "unsupported --order"},
			{" start", "asc", "unsupported --sort"}, {"start", "desc ", "unsupported --order"},
		} {
			for _, backendErr := range []error{nil, errors.New("backend unavailable")} {
				t.Run(tc.sort+"/"+tc.order, func(t *testing.T) {
					b := &limitAwareQueryBackend{scopeCaptureBackend: scopeCaptureBackend{listErr: backendErr}}
					installQueryBackend(t, b)
					q := savedQuery{Name: "invalid", From: "2026-02-20", To: "2026-02-22", Sort: tc.sort, Order: tc.order}
					if saved {
						cmd := NewRootCommand()
						cmd.SetOut(io.Discard)
						cmd.SetErr(io.Discard)
						cmd.SetArgs([]string{"queries", "save", q.Name, "--from", q.From, "--to", q.To, "--sort", q.Sort, "--order", q.Order})
						if err := cmd.Execute(); err != nil {
							t.Fatalf("save must remain permissive: %v", err)
						}
						store, loadErr := loadSavedQueries()
						if loadErr != nil || store[q.Name].Sort != q.Sort || store[q.Name].Order != q.Order {
							t.Fatalf("saved sort changed: %+v %v", store, loadErr)
						}
					}
					out, err := runQueryCommand(t, q, saved)
					if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.want) || b.calls != 0 {
						t.Fatalf("calls=%d err=%v output=%s", b.calls, err, out)
					}
					var got contract.ErrorEnvelope
					if err := json.Unmarshal(out, &got); err != nil {
						t.Fatal(err)
					}
					if got.Error.Code != contract.ErrInvalidUsage {
						t.Fatalf("wrong error: %s", out)
					}
				})
			}
		}
	}
}

func TestQuerySupportedSorts(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, field := range []string{"start", "end", "title", "updated_at", "calendar", "TITLE", ""} {
			for _, order := range []string{"asc", "desc", "DESC", ""} {
				t.Run(field+"/"+order, func(t *testing.T) {
					b := &limitAwareQueryBackend{}
					installQueryBackend(t, b)
					out, err := runQueryCommand(t, savedQuery{Name: "sort", From: "2026-02-20", To: "2026-02-22", Sort: field, Order: order}, saved)
					if !saved && (field == "" || order == "") {
						if err == nil || ExitCode(err) != 2 || b.calls != 0 {
							t.Fatalf("empty direct sort must fail: %v %s", err, out)
						}
					} else if err != nil || b.calls != 1 {
						t.Fatalf("calls=%d err=%v output=%s", b.calls, err, out)
					}
				})
			}
		}
	}
}
