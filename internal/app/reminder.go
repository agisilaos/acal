package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var reminderLineRE = regexp.MustCompile(`(?m)^acal:reminder=([+-]?[0-9]+[smhd])\s*$`)

func normalizeReminderOffset(v string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, err
	}
	if d == 0 {
		return 0, fmt.Errorf("offset must not be zero")
	}
	if d > 0 {
		d = -d
	}
	return d, nil
}

func setReminderMarker(notes string, offset time.Duration) string {
	clean := clearReminderMarker(notes)
	marker := fmt.Sprintf("acal:reminder=%s", offset.String())
	clean = strings.TrimRight(clean, "\n")
	if clean == "" {
		return marker
	}
	return clean + "\n" + marker
}

func clearReminderMarker(notes string) string {
	lines := strings.Split(notes, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if reminderLineRE.MatchString(strings.TrimSpace(line)) {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}
