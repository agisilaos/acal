package app

import (
	"fmt"
	"strings"
	"time"
)

func normalizeReminderOffset(v string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, err
	}
	if d == 0 {
		return 0, fmt.Errorf("offset must not be zero")
	}
	if d%time.Minute != 0 {
		return 0, fmt.Errorf("offset must be a whole number of minutes")
	}
	if d > 0 {
		d = -d
	}
	return d, nil
}
