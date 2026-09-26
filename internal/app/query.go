package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type queryStage uint8

const (
	queryFetch queryStage = iota
	queryParse
	queryApply
)

type queryError struct {
	stage queryStage
	err   error
}

func (e *queryError) Error() string { return e.err.Error() }
func (e *queryError) Unwrap() error { return e.err }

// executeQuery limits results only after filtering and sorting the complete range.
func executeQuery(ctx context.Context, be backend.Backend, filter backend.EventFilter, wheres []string, sortField, order string) ([]contract.Event, *queryError) {
	preds, err := parsePredicates(wheres)
	if err != nil {
		return nil, &queryError{stage: queryParse, err: err}
	}
	matchers, err := compilePredicates(preds)
	if err != nil {
		return nil, &queryError{stage: queryApply, err: err}
	}
	limit := filter.Limit
	filter.Limit = 0
	items, err := listEventsWithTimeout(ctx, be, filter)
	if err != nil {
		return nil, &queryError{stage: queryFetch, err: err}
	}
	items = applyPredicates(items, matchers)
	sortEvents(items, sortField, order)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

type predicate struct {
	field string
	op    string
	value string
}

func parsePredicates(wheres []string) ([]predicate, error) {
	out := make([]predicate, 0, len(wheres))
	ops := []string{"==", "!=", "~", ">=", "<=", ">", "<"}
	for _, w := range wheres {
		s := strings.TrimSpace(w)
		if s == "" {
			continue
		}
		var op string
		var idx int
		for _, candidate := range ops {
			if i := strings.Index(s, candidate); i > 0 {
				op = candidate
				idx = i
				break
			}
		}
		if op == "" {
			return nil, fmt.Errorf("invalid where clause: %s", w)
		}
		field := strings.TrimSpace(s[:idx])
		val := strings.Trim(strings.TrimSpace(s[idx+len(op):]), "\"")
		if field == "" || val == "" {
			return nil, fmt.Errorf("invalid where clause: %s", w)
		}
		out = append(out, predicate{field: strings.ToLower(field), op: op, value: val})
	}
	return out, nil
}

type eventMatcher func(contract.Event) bool

func compilePredicates(preds []predicate) ([]eventMatcher, error) {
	matchers := make([]eventMatcher, 0, len(preds))
	for _, p := range preds {
		matcher, err := compilePredicate(p)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)
	}
	return matchers, nil
}

func compilePredicate(p predicate) (eventMatcher, error) {
	var field func(contract.Event) string
	switch p.field {
	case "title":
		field = func(e contract.Event) string { return e.Title }
	case "calendar", "calendar_name":
		field = func(e contract.Event) string { return e.CalendarName }
	case "calendar_id":
		field = func(e contract.Event) string { return e.CalendarID }
	case "location":
		field = func(e contract.Event) string { return e.Location }
	case "notes":
		field = func(e contract.Event) string { return e.Notes }
	case "id":
		field = func(e contract.Event) string { return e.ID }
	case "start":
		return compileTimePredicate(func(e contract.Event) time.Time { return e.Start }, p)
	case "end":
		return compileTimePredicate(func(e contract.Event) time.Time { return e.End }, p)
	default:
		return nil, fmt.Errorf("unsupported field in --where: %s", p.field)
	}

	expected := strings.ToLower(p.value)
	switch p.op {
	case "==":
		return func(e contract.Event) bool { return strings.ToLower(field(e)) == expected }, nil
	case "!=":
		return func(e contract.Event) bool { return strings.ToLower(field(e)) != expected }, nil
	case "~":
		return func(e contract.Event) bool { return strings.Contains(strings.ToLower(field(e)), expected) }, nil
	default:
		return nil, fmt.Errorf("operator %s not supported for string fields", p.op)
	}
}

func compileTimePredicate(field func(contract.Event) time.Time, p predicate) (eventMatcher, error) {
	expected, err := time.Parse(time.RFC3339, p.value)
	if err != nil {
		return nil, fmt.Errorf("time predicate expects RFC3339 value, got %q", p.value)
	}
	switch p.op {
	case "==":
		return func(e contract.Event) bool { return field(e).Equal(expected) }, nil
	case "!=":
		return func(e contract.Event) bool { return !field(e).Equal(expected) }, nil
	case ">":
		return func(e contract.Event) bool { return field(e).After(expected) }, nil
	case ">=":
		return func(e contract.Event) bool { return !field(e).Before(expected) }, nil
	case "<":
		return func(e contract.Event) bool { return field(e).Before(expected) }, nil
	case "<=":
		return func(e contract.Event) bool { return !field(e).After(expected) }, nil
	default:
		return nil, fmt.Errorf("operator %s not supported for time fields", p.op)
	}
}

func applyPredicates(items []contract.Event, matchers []eventMatcher) []contract.Event {
	filtered := make([]contract.Event, 0, len(items))
	for _, e := range items {
		if matchesAll(e, matchers) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func matchesAll(e contract.Event, matchers []eventMatcher) bool {
	for _, match := range matchers {
		if !match(e) {
			return false
		}
	}
	return true
}

func sortEvents(items []contract.Event, sortField, order string) {
	desc := strings.EqualFold(order, "desc")
	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			i, j = j, i
		}
		var less bool
		switch strings.ToLower(sortField) {
		case "title":
			less = items[i].Title < items[j].Title
		case "end":
			less = items[i].End.Before(items[j].End)
		case "updated_at":
			less = items[i].UpdatedAt.Before(items[j].UpdatedAt)
		case "calendar":
			less = items[i].CalendarName < items[j].CalendarName
		default:
			less = items[i].Start.Before(items[j].Start)
		}
		return less
	})
}
