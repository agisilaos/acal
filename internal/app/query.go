package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

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

func applyPredicates(items []contract.Event, preds []predicate) ([]contract.Event, error) {
	filtered := make([]contract.Event, 0, len(items))
	for _, e := range items {
		ok, err := matchesAll(e, preds)
		if err != nil {
			return nil, err
		}
		if ok {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

func matchesAll(e contract.Event, preds []predicate) (bool, error) {
	for _, p := range preds {
		ok, err := matchesOne(e, p)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func matchesOne(e contract.Event, p predicate) (bool, error) {
	switch p.field {
	case "title":
		return compareString(e.Title, p.op, p.value)
	case "calendar", "calendar_name":
		return compareString(e.CalendarName, p.op, p.value)
	case "calendar_id":
		return compareString(e.CalendarID, p.op, p.value)
	case "location":
		return compareString(e.Location, p.op, p.value)
	case "notes":
		return compareString(e.Notes, p.op, p.value)
	case "id":
		return compareString(e.ID, p.op, p.value)
	case "start":
		return compareTime(e.Start, p.op, p.value)
	case "end":
		return compareTime(e.End, p.op, p.value)
	default:
		return false, fmt.Errorf("unsupported field in --where: %s", p.field)
	}
}

func compareString(actual, op, expected string) (bool, error) {
	a := strings.ToLower(actual)
	e := strings.ToLower(expected)
	switch op {
	case "==":
		return a == e, nil
	case "!=":
		return a != e, nil
	case "~":
		return strings.Contains(a, e), nil
	default:
		return false, fmt.Errorf("operator %s not supported for string fields", op)
	}
}

func compareTime(actual time.Time, op, expected string) (bool, error) {
	parsed, err := time.Parse(time.RFC3339, expected)
	if err != nil {
		return false, fmt.Errorf("time predicate expects RFC3339 value, got %q", expected)
	}
	switch op {
	case "==":
		return actual.Equal(parsed), nil
	case "!=":
		return !actual.Equal(parsed), nil
	case ">":
		return actual.After(parsed), nil
	case ">=":
		return actual.After(parsed) || actual.Equal(parsed), nil
	case "<":
		return actual.Before(parsed), nil
	case "<=":
		return actual.Before(parsed) || actual.Equal(parsed), nil
	default:
		return false, fmt.Errorf("operator %s not supported for time fields", op)
	}
}

func sortEvents(items []contract.Event, sortField, order string) {
	desc := strings.EqualFold(order, "desc")
	sort.Slice(items, func(i, j int) bool {
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
		if desc {
			return !less
		}
		return less
	})
}
