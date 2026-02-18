package backend

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (b *OsaScriptBackend) GetReminderOffset(ctx context.Context, id string) (*time.Duration, error) {
	uid, occ := parseEventID(id)
	if strings.TrimSpace(uid) == "" {
		return nil, fmt.Errorf("invalid event id")
	}
	occUnix := "0"
	if occ > 0 {
		occUnix = strconv.FormatInt(occ+cocoaEpochOffset, 10)
	}
	out, err := runAppleScript(ctx, []string{
		`on run argv`,
		`set uidText to item 1 of argv`,
		`set occUnix to item 2 of argv as integer`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`tell application "Calendar"`,
		`repeat with c in calendars`,
		`set targetEvent to missing value`,
		`try`,
		`if occUnix > 0 then`,
		`set targetEvent to first event of c whose uid is uidText and ((start date of it - epoch) as integer) is occUnix`,
		`else`,
		`set targetEvent to first event of c whose uid is uidText`,
		`end if`,
		`on error`,
		`set targetEvent to missing value`,
		`end try`,
		`if targetEvent is not missing value then`,
		`if (count of display alarms of targetEvent) is 0 then return "NONE"`,
		`set a to first display alarm of targetEvent`,
		`return (trigger interval of a as text)`,
		`end if`,
		`end repeat`,
		`error "event not found"`,
		`end tell`,
		`end run`,
	}, uid, occUnix)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(trimOuterQuotes(strings.TrimSpace(out)))
	if strings.EqualFold(s, "NONE") || s == "" {
		return nil, nil
	}
	mins, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("invalid reminder trigger interval: %s", s)
	}
	d := time.Duration(mins) * time.Minute
	return &d, nil
}
