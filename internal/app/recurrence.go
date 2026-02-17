package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type repeatSpec struct {
	Frequency string
	Weekdays  []time.Weekday
	Count     int
}

func parseRepeatSpec(v string, anchor time.Time) (repeatSpec, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return repeatSpec{}, nil
	}
	count := 0
	if strings.Contains(s, "*") {
		parts := strings.SplitN(s, "*", 2)
		s = strings.TrimSpace(parts[0])
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || n <= 0 {
			return repeatSpec{}, fmt.Errorf("invalid repeat count")
		}
		count = n
	}
	sp := repeatSpec{Count: count}
	left := s
	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		left = strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left == "weekly" {
			ws, err := parseWeekdays(right)
			if err != nil {
				return repeatSpec{}, err
			}
			sp.Weekdays = ws
		}
	}
	sp.Frequency = left
	if sp.Frequency == "weekly" && len(sp.Weekdays) == 0 {
		sp.Weekdays = []time.Weekday{anchor.Weekday()}
	}
	switch sp.Frequency {
	case "daily", "weekly", "monthly", "yearly":
		if sp.Count == 0 {
			sp.Count = 10
		}
		return sp, nil
	default:
		return repeatSpec{}, fmt.Errorf("unsupported --repeat frequency")
	}
}

func expandRepeat(st time.Time, spec repeatSpec) []time.Time {
	if spec.Frequency == "" {
		return []time.Time{st}
	}
	out := make([]time.Time, 0, spec.Count)
	out = append(out, st)
	for len(out) < spec.Count {
		last := out[len(out)-1]
		switch spec.Frequency {
		case "daily":
			out = append(out, last.AddDate(0, 0, 1))
		case "weekly":
			next := nextWeeklyOccurrence(last, spec.Weekdays)
			out = append(out, next)
		case "monthly":
			out = append(out, last.AddDate(0, 1, 0))
		case "yearly":
			out = append(out, last.AddDate(1, 0, 0))
		}
	}
	return out
}

func parseWeekdays(v string) ([]time.Weekday, error) {
	parts := strings.Split(v, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("weekly repeat requires weekdays")
	}
	out := make([]time.Weekday, 0, len(parts))
	seen := map[time.Weekday]bool{}
	for _, p := range parts {
		wd, err := parseWeekdayToken(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		if !seen[wd] {
			out = append(out, wd)
			seen[wd] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("weekly repeat requires weekdays")
	}
	return out, nil
}

func parseWeekdayToken(v string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "mon", "monday":
		return time.Monday, nil
	case "tue", "tues", "tuesday":
		return time.Tuesday, nil
	case "wed", "wednesday":
		return time.Wednesday, nil
	case "thu", "thurs", "thursday":
		return time.Thursday, nil
	case "fri", "friday":
		return time.Friday, nil
	case "sat", "saturday":
		return time.Saturday, nil
	case "sun", "sunday":
		return time.Sunday, nil
	default:
		return time.Sunday, fmt.Errorf("invalid weekday: %s", v)
	}
}

func nextWeeklyOccurrence(cur time.Time, wds []time.Weekday) time.Time {
	for i := 1; i <= 7; i++ {
		cand := cur.AddDate(0, 0, i)
		for _, wd := range wds {
			if cand.Weekday() == wd {
				return cand
			}
		}
	}
	return cur.AddDate(0, 0, 7)
}

func setRepeatMarker(notes, repeat string) string {
	clean := clearRepeatMarker(notes)
	marker := fmt.Sprintf("acal:repeat=%s", strings.TrimSpace(repeat))
	clean = strings.TrimRight(clean, "\n")
	if clean == "" {
		return marker
	}
	return clean + "\n" + marker
}

func clearRepeatMarker(notes string) string {
	lines := strings.Split(notes, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "acal:repeat=") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}
