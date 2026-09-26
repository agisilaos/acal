package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
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
	got, err := applyPredicates(items, preds)
	if err != nil {
		t.Fatalf("applyPredicates error: %v", err)
	}
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
	got, err := applyPredicates(items, preds)
	if err != nil {
		t.Fatalf("applyPredicates error: %v", err)
	}
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

func TestSortEventsPreservesTies(t *testing.T) {
	for _, field := range []string{"start", "end", "title", "updated_at", "calendar"} {
		for _, order := range []string{"asc", "desc"} {
			t.Run(field+"/"+order, func(t *testing.T) {
				// More than a small-sort cutoff ensures ties survive larger sorts too.
				items := make([]contract.Event, 40)
				for i := range items {
					items[i].ID = fmt.Sprint(i)
				}
				sortEvents(items, field, order)
				for i, item := range items {
					if item.ID != fmt.Sprint(i) {
						t.Fatalf("tie moved at %d: got %s", i, item.ID)
					}
				}
			})
		}
	}
}

func TestExecuteQueryPreservesContextErrors(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			b := &scopeCaptureBackend{listErr: cause}
			_, err := executeQuery(context.Background(), b, backend.EventFilter{Limit: 1}, nil, "start", "asc")
			if err == nil || !errors.Is(err, cause) {
				t.Fatalf("got %v; want wrapped %v", err, cause)
			}
		})
	}
}
