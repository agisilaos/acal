package app

import (
	"testing"
	"time"

	"github.com/agis/acal/internal/contract"
)

func TestParsePredicates(t *testing.T) {
	preds, err := parsePredicates([]string{"title~sleep", "calendar==Personal"})
	if err != nil {
		t.Fatalf("parsePredicates error: %v", err)
	}
	if len(preds) != 2 {
		t.Fatalf("expected 2 predicates, got %d", len(preds))
	}
	if preds[0].field != "title" || preds[0].op != "~" || preds[0].value != "sleep" {
		t.Fatalf("unexpected first predicate: %+v", preds[0])
	}
}

func TestParsePredicatesInvalid(t *testing.T) {
	if _, err := parsePredicates([]string{"badclause"}); err == nil {
		t.Fatalf("expected error for invalid predicate")
	}
}

func TestApplyPredicates(t *testing.T) {
	items := []contract.Event{
		{Title: "Sleep", CalendarName: "Personal", Start: mustRFC3339(t, "2026-02-08T22:00:00+01:00")},
		{Title: "Work Session", CalendarName: "Work", Start: mustRFC3339(t, "2026-02-08T10:00:00+01:00")},
	}
	preds, err := parsePredicates([]string{"title~sleep", "calendar==Personal"})
	if err != nil {
		t.Fatalf("parsePredicates error: %v", err)
	}
	matchers, err := compilePredicates(preds)
	if err != nil {
		t.Fatalf("compilePredicates error: %v", err)
	}
	got := applyPredicates(items, matchers)
	if len(got) != 1 || got[0].Title != "Sleep" {
		t.Fatalf("unexpected filtered results: %+v", got)
	}
}

func TestApplyPredicatesTimeComparison(t *testing.T) {
	items := []contract.Event{{Title: "A", Start: mustRFC3339(t, "2026-02-08T22:00:00+01:00")}}
	preds, err := parsePredicates([]string{"start>=2026-02-08T21:00:00+01:00"})
	if err != nil {
		t.Fatalf("parsePredicates error: %v", err)
	}
	matchers, err := compilePredicates(preds)
	if err != nil {
		t.Fatalf("compilePredicates error: %v", err)
	}
	got := applyPredicates(items, matchers)
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
}

func TestSortEvents(t *testing.T) {
	items := []contract.Event{
		{Title: "B", Start: mustRFC3339(t, "2026-02-08T22:00:00+01:00")},
		{Title: "A", Start: mustRFC3339(t, "2026-02-08T10:00:00+01:00")},
	}
	sortEvents(items, "title", "asc")
	if items[0].Title != "A" {
		t.Fatalf("expected A first, got %s", items[0].Title)
	}
	sortEvents(items, "title", "desc")
	if items[0].Title != "B" {
		t.Fatalf("expected B first, got %s", items[0].Title)
	}
}

func mustRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("time parse failed: %v", err)
	}
	return v
}

func TestCompiledStringPredicates(t *testing.T) {
	fields := []struct {
		name  string
		event contract.Event
	}{
		{"title", contract.Event{Title: "MiXeD Value"}},
		{"calendar", contract.Event{CalendarName: "MiXeD Value"}},
		{"calendar_name", contract.Event{CalendarName: "MiXeD Value"}},
		{"calendar_id", contract.Event{CalendarID: "MiXeD Value"}},
		{"location", contract.Event{Location: "MiXeD Value"}},
		{"notes", contract.Event{Notes: "MiXeD Value"}},
		{"id", contract.Event{ID: "MiXeD Value"}},
	}
	wantByOperator := map[string][3]bool{
		"==": {true, false, false},
		"!=": {false, true, true},
		"~":  {true, true, false},
	}
	for _, field := range fields {
		for _, op := range []string{"==", "!=", "~", ">", ">=", "<", "<="} {
			t.Run(field.name+op, func(t *testing.T) {
				want, supported := wantByOperator[op]
				for i, value := range []string{"mixed VALUE", "XeD", "other"} {
					matchers, err := compilePredicates([]predicate{{field: field.name, op: op, value: value}})
					if !supported {
						if err == nil {
							t.Fatal("expected unsupported string operator error")
						}
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := matchesAll(field.event, matchers); got != want[i] {
						t.Fatalf("value %q: got %v, want %v", value, got, want[i])
					}
				}
			})
		}
	}
}

func TestCompiledTimePredicates(t *testing.T) {
	base := mustRFC3339(t, "2026-02-08T21:00:00Z")
	for _, field := range []string{"start", "end"} {
		for _, tc := range []struct {
			op   string
			want [3]bool // Event before, equal to, and after the constant.
		}{
			{"==", [3]bool{false, true, false}},
			{"!=", [3]bool{true, false, true}},
			{">", [3]bool{false, false, true}},
			{">=", [3]bool{false, true, true}},
			{"<", [3]bool{true, false, false}},
			{"<=", [3]bool{true, true, false}},
			{"~", [3]bool{}},
		} {
			t.Run(field+tc.op, func(t *testing.T) {
				matchers, err := compilePredicates([]predicate{{field: field, op: tc.op, value: "2026-02-08T22:00:00+01:00"}})
				if tc.op == "~" {
					if err == nil {
						t.Fatal("expected unsupported time operator error")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				for i, delta := range []time.Duration{-time.Second, 0, time.Second} {
					event := contract.Event{}
					if field == "start" {
						event.Start = base.Add(delta)
					} else {
						event.End = base.Add(delta)
					}
					if got := matchesAll(event, matchers); got != tc.want[i] {
						t.Fatalf("delta %v: got %v, want %v", delta, got, tc.want[i])
					}
				}
			})
		}
	}
}

func TestPredicateGrammarCompatibility(t *testing.T) {
	for _, tc := range []struct {
		clause string
		want   predicate
	}{
		{`  TiTLe ~ "MiXeD Value"  `, predicate{"title", "~", "MiXeD Value"}},
		{`title==" spaced "`, predicate{"title", "==", " spaced "}},
		{`title==""unbalanced"`, predicate{"title", "==", "unbalanced"}},
		{`title=='literal'`, predicate{"title", "==", "'literal'"}},
		{`title~a==b`, predicate{"title~a", "==", "b"}},
	} {
		t.Run(tc.clause, func(t *testing.T) {
			preds, err := parsePredicates([]string{"", " \t ", tc.clause})
			if err != nil || len(preds) != 1 || preds[0] != tc.want {
				t.Fatalf("got %+v, %v; want %+v", preds, err, tc.want)
			}
		})
	}
	for _, clause := range []string{"badclause", "==x", "title==", `title==""`, " ==x"} {
		if _, err := parsePredicates([]string{clause}); err == nil {
			t.Errorf("expected invalid syntax for %q", clause)
		}
	}
	preds, err := parsePredicates([]string{"", " \t "})
	if err != nil {
		t.Fatal(err)
	}
	matchers, err := compilePredicates(preds)
	if err != nil {
		t.Fatal(err)
	}
	if got := applyPredicates([]contract.Event{{ID: "keep"}}, matchers); len(got) != 1 {
		t.Fatalf("empty clauses must match everything: %+v", got)
	}
	if got := applyPredicates(nil, matchers); got == nil || len(got) != 0 {
		t.Fatalf("empty results must remain a non-nil slice: %+v", got)
	}
}
