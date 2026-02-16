package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ParseDateTime(input string, now time.Time, loc *time.Location) (time.Time, error) {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}

	switch s {
	case "today":
		y, m, d := now.In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, loc), nil
	case "tomorrow":
		v, _ := ParseDateTime("today", now, loc)
		return v.Add(24 * time.Hour), nil
	case "yesterday":
		v, _ := ParseDateTime("today", now, loc)
		return v.Add(-24 * time.Hour), nil
	}

	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		sign := 1
		if strings.HasPrefix(s, "-") {
			sign = -1
		}
		raw := strings.TrimPrefix(strings.TrimPrefix(s, "+"), "-")
		if strings.HasSuffix(raw, "d") {
			n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid relative day: %s", input)
			}
			v, _ := ParseDateTime("today", now, loc)
			return v.Add(time.Duration(sign*n) * 24 * time.Hour), nil
		}
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, input, loc); err == nil {
			return ts, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported datetime format: %s", input)
}
