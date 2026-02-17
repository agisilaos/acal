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
	if strings.Count(s, "*") > 1 {
		return repeatSpec{}, fmt.Errorf("invalid repeat rule: too many '*' segments")
	}
	if strings.Count(s, ":") > 1 {
		return repeatSpec{}, fmt.Errorf("invalid repeat rule: too many ':' segments")
	}
	count := 0
	if strings.Contains(s, "*") {
		parts := strings.SplitN(s, "*", 2)
		if strings.TrimSpace(parts[0]) == "" {
			return repeatSpec{}, fmt.Errorf("invalid repeat rule: missing frequency")
		}
		s = strings.TrimSpace(parts[0])
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || n <= 0 {
			return repeatSpec{}, fmt.Errorf("invalid repeat count")
		}
		if n > 366 {
			return repeatSpec{}, fmt.Errorf("repeat count too large (max 366)")
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
		} else {
			return repeatSpec{}, fmt.Errorf("only weekly supports weekday selectors")
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

func canonicalRepeatRule(spec repeatSpec) string {
	if spec.Frequency == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(spec.Frequency)
	if spec.Frequency == "weekly" && len(spec.Weekdays) > 0 {
		b.WriteString(":")
		names := make([]string, 0, len(spec.Weekdays))
		for _, wd := range spec.Weekdays {
			names = append(names, canonicalWeekdayToken(wd))
		}
		b.WriteString(strings.Join(names, ","))
	}
	if spec.Count > 0 {
		b.WriteString("*")
		b.WriteString(strconv.Itoa(spec.Count))
	}
	return b.String()
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
	// Deterministic canonical ordering for stable round-tripping.
	order := map[time.Weekday]int{
		time.Monday:    0,
		time.Tuesday:   1,
		time.Wednesday: 2,
		time.Thursday:  3,
		time.Friday:    4,
		time.Saturday:  5,
		time.Sunday:    6,
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if order[out[j]] < order[out[i]] {
				out[i], out[j] = out[j], out[i]
			}
		}
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

func canonicalWeekdayToken(wd time.Weekday) string {
	switch wd {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	case time.Saturday:
		return "sat"
	case time.Sunday:
		return "sun"
	default:
		return "sun"
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
