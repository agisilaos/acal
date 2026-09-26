package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

// Unlike scopeCaptureBackend, this fake reproduces backend truncation.
type limitAwareQueryBackend struct {
	scopeCaptureBackend
	calls int
}

func (b *limitAwareQueryBackend) ListEvents(ctx context.Context, f backend.EventFilter) ([]contract.Event, error) {
	b.calls++
	b.lastFilter = f
	items, err := b.scopeCaptureBackend.ListEvents(ctx, f)
	if f.Limit > 0 && len(items) > f.Limit {
		items = items[:f.Limit]
	}
	return items, err
}

func installQueryBackend(t *testing.T, b backend.Backend) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	orig := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return b, nil }
	t.Cleanup(func() { backendFactory = orig })
}

func runQueryCommand(t *testing.T, q savedQuery, saved bool) ([]byte, error) {
	t.Helper()
	args := []string{"events", "query", "--from", q.From, "--to", q.To,
		"--sort", q.Sort, "--order", q.Order, "--limit", strconv.Itoa(q.Limit)}
	for _, calendar := range q.Calendars {
		args = append(args, "--calendar", calendar)
	}
	for _, where := range q.Wheres {
		args = append(args, "--where", where)
	}
	if saved {
		if err := writeSavedQueries(map[string]savedQuery{q.Name: q}); err != nil {
			t.Fatal(err)
		}
		args = []string{"queries", "run", q.Name}
	}
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append(args, "--tz", "UTC", "--json"))
	err := cmd.Execute()
	return out.Bytes(), err
}

func TestQueryResultLimits(t *testing.T) {
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	events := []contract.Event{
		{ID: "early", Title: "Zulu", Start: base},
		{ID: "later", Title: "Alpha", Start: base.Add(time.Hour)},
		{ID: "tie", Title: "Alpha", Start: base.Add(2 * time.Hour)},
	}
	for _, saved := range []bool{false, true} {
		mode := "direct"
		if saved {
			mode = "saved"
		}
		for _, tc := range []struct {
			name, where, sort, order string
			limit                    int
			empty                    bool
			want                     []string
		}{
			{name: "later match", where: "title~Alpha", sort: "start", order: "asc", limit: 1, want: []string{"later"}},
			{name: "title before limit", sort: "title", order: "asc", limit: 1, want: []string{"later"}},
			{name: "descending before limit", sort: "start", order: "desc", limit: 1, want: []string{"tie"}},
			{name: "descending ties", where: "title~Alpha", sort: "title", order: "desc", limit: 1, want: []string{"later"}},
			{name: "zero unlimited", sort: "title", order: "asc", want: []string{"later", "tie", "early"}},
			{name: "negative unlimited", sort: "title", order: "asc", limit: -1, want: []string{"later", "tie", "early"}},
			{name: "under limit", where: "title~Alpha", sort: "start", order: "asc", limit: 10, want: []string{"later", "tie"}},
			{name: "no matches", where: "title~missing", sort: "start", order: "asc", limit: 1, want: []string{}},
			{name: "empty backend", empty: true, sort: "start", order: "asc", limit: 1, want: []string{}},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				b := &limitAwareQueryBackend{scopeCaptureBackend: scopeCaptureBackend{events: events}}
				if tc.empty {
					b.events = nil
				}
				installQueryBackend(t, b)
				q := savedQuery{Name: "example", From: "2026-02-20", To: "2026-02-22", Calendars: []string{"Work"}, Sort: tc.sort, Order: tc.order, Limit: tc.limit}
				if tc.where != "" {
					q.Wheres = []string{tc.where}
				}
				out, err := runQueryCommand(t, q, saved)
				if err != nil {
					t.Fatalf("query: %v\n%s", err, out)
				}
				var got struct {
					Data []contract.Event `json:"data"`
					Meta struct {
						Count int    `json:"count"`
						Name  string `json:"name"`
					} `json:"meta"`
				}
				if err := json.Unmarshal(out, &got); err != nil {
					t.Fatal(err)
				}
				ids := make([]string, 0, len(got.Data))
				for _, event := range got.Data {
					ids = append(ids, event.ID)
				}
				if !reflect.DeepEqual(ids, tc.want) || got.Data == nil || got.Meta.Count != len(tc.want) {
					t.Fatalf("got %s; want IDs %v and count %d", out, tc.want, len(tc.want))
				}
				if (saved && got.Meta.Name != q.Name) || (!saved && got.Meta.Name != "") {
					t.Fatalf("unexpected name metadata: %s", out)
				}
				wantFilter, err := buildEventFilterWithTZ(q.From, q.To, q.Calendars, 0, "UTC")
				if err != nil {
					t.Fatal(err)
				}
				if b.calls != 1 || !reflect.DeepEqual(b.lastFilter, wantFilter) {
					t.Fatalf("backend calls=%d filter=%+v; want one call with %+v", b.calls, b.lastFilter, wantFilter)
				}
			})
		}
	}
}

func TestQueryErrors(t *testing.T) {
	for _, saved := range []bool{false, true} {
		mode := "direct"
		if saved {
			mode = "saved"
		}
		for _, tc := range []struct {
			name, where, directHint, savedHint, kind string
			backendErr                               error
			invalidRange                             bool
			code                                     contract.ErrorCode
			exit                                     int
		}{
			{name: "range", invalidRange: true, directHint: "Use valid --from/--to values", savedHint: "Saved query has invalid range; re-save it", code: contract.ErrInvalidUsage, exit: 2},
			{name: "parse", where: "badclause", directHint: `Use clauses like title~"walk" or calendar=="Work"`, savedHint: "Saved query has invalid predicates; re-save it", code: contract.ErrInvalidUsage, exit: 2},
			{name: "apply", where: "unknown==x", directHint: "Check --where field/operator/value", savedHint: "Saved query predicates failed; re-save it", code: contract.ErrInvalidUsage, exit: 2},
			{name: "parse precedes backend", where: "badclause", backendErr: errors.New("backend unavailable"), directHint: `Use clauses like title~"walk" or calendar=="Work"`, savedHint: "Saved query has invalid predicates; re-save it", code: contract.ErrInvalidUsage, exit: 2},
			{name: "backend", backendErr: errors.New("backend unavailable"), directHint: "Run `acal doctor` for remediation", code: contract.ErrBackendUnavailable, exit: 6},
			{name: "timeout", backendErr: context.DeadlineExceeded, directHint: "Retry with a higher --timeout or run `acal doctor` for remediation", kind: "timeout", code: contract.ErrBackendUnavailable, exit: 6},
			{name: "canceled", backendErr: context.Canceled, directHint: "Retry command; operation was canceled", kind: "canceled", code: contract.ErrBackendUnavailable, exit: 6},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				b := &limitAwareQueryBackend{scopeCaptureBackend: scopeCaptureBackend{events: []contract.Event{{ID: "event"}}, listErr: tc.backendErr}}
				installQueryBackend(t, b)
				q := savedQuery{Name: "example", From: "2026-02-20", To: "2026-02-22", Sort: "start", Order: "asc", Wheres: []string{tc.where}, Limit: 1}
				if tc.invalidRange {
					q.From = "invalid"
				}
				out, err := runQueryCommand(t, q, saved)
				if err == nil || ExitCode(err) != tc.exit {
					t.Fatalf("got err=%v output=%s; want exit %d", err, out, tc.exit)
				}
				var got contract.ErrorEnvelope
				if err := json.Unmarshal(out, &got); err != nil {
					t.Fatal(err)
				}
				wantHint := tc.directHint
				if saved && tc.savedHint != "" {
					wantHint = tc.savedHint
				}
				if got.Error.Code != tc.code || got.Error.Hint != wantHint {
					t.Fatalf("got %s; want code %s hint %q", out, tc.code, wantHint)
				}
				if tc.kind != "" && (got.Meta["kind"] != tc.kind || got.Meta["phase"] != "backend.list_events") {
					t.Fatalf("missing backend classification: %s", out)
				}
				if tc.code == contract.ErrInvalidUsage && b.calls != 0 {
					t.Fatalf("backend called for invalid query")
				}
			})
		}
	}
}

func TestListAndSearchKeepBackendLimit(t *testing.T) {
	for _, command := range [][]string{{"events", "list"}, {"events", "search", "meeting"}} {
		t.Run(command[1], func(t *testing.T) {
			b := &limitAwareQueryBackend{}
			installQueryBackend(t, b)
			cmd := NewRootCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append(command, "--from", "2026-02-20", "--to", "2026-02-22", "--limit", "1", "--json"))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if b.calls != 1 || b.lastFilter.Limit != 1 {
				t.Fatalf("backend calls=%d limit=%d; want 1 each", b.calls, b.lastFilter.Limit)
			}
		})
	}
}
