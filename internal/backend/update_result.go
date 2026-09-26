package backend

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

// UpdateOutcomeError means callers must inspect Calendar before retrying. Applied
// is true only after the update script completed; false means outcome unknown.
// Neither outcome provides a snapshot suitable for history.
type UpdateOutcomeError struct {
	Applied bool
	Err     error
}

func (e *UpdateOutcomeError) Error() string {
	if e.Applied {
		return fmt.Sprintf("update applied but result unverified: %v", e.Err)
	}
	return fmt.Sprintf("update outcome unknown; Calendar may have changed: %v", e.Err)
}
func (e *UpdateOutcomeError) Unwrap() error { return e.Err }

// Read the actual mutated reference in the write script, bypassing the lagging
// occurrence cache. JSON preserves empty values, whitespace and control characters.
func updateResultScriptHandlers() []string {
	return []string{
		`use framework "Foundation"`,
		`use scripting additions`,
		`on unixSeconds(nativeDate)`,
		`set instant to current application's NSDate's dateWithTimeInterval:0 sinceDate:nativeDate`,
		`set secondsValue to current application's NSNumber's numberWithDouble:(instant's timeIntervalSince1970())`,
		`return secondsValue's stringValue() as text`,
		`end unixSeconds`,
		`on nativeTimezone()`,
		`set tz to current application's NSTimeZone's |localTimeZone|()`,
		`return tz's |name|() as text`,
		`end nativeTimezone`,
		`on optionalText(v)`,
		`if v is missing value then return ""`,
		`return v as text`,
		`end optionalText`,
		`on jsonString(v)`,
		`set encoded to ""`,
		`set hexDigits to "0123456789abcdef"`,
		`repeat with ch in characters of (v as text)`,
		`set n to id of ch`,
		`if ch as text is "\\" then`,
		`set encoded to encoded & "\\\\"`,
		`else if ch as text is quote then`,
		`set encoded to encoded & "\\" & quote`,
		`else if n < 32 then`,
		`set encoded to encoded & "\\u00" & character ((n div 16) + 1) of hexDigits & character ((n mod 16) + 1) of hexDigits`,
		`else`,
		`set encoded to encoded & ch`,
		`end if`,
		`end repeat`,
		`return quote & encoded & quote`,
		`end jsonString`,
		`on resultJSON(fields)`,
		`set encoded to "["`,
		`set separator to ""`,
		`repeat with fieldValue in fields`,
		`set encoded to encoded & separator & my jsonString(contents of fieldValue)`,
		`set separator to ","`,
		`end repeat`,
		`return encoded & "]"`,
		`end resultJSON`,
	}
}

func decodeUpdateResult(out, uid string, occurrence int64, scope RecurrenceScope, in EventUpdateInput) (*contract.Event, error) {
	var fields []string
	if err := json.Unmarshal([]byte(out), &fields); err != nil || len(fields) != 13 {
		return nil, fmt.Errorf("complete native event readback unavailable")
	}
	start, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid native start")
	}
	end, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil || end <= start {
		return nil, fmt.Errorf("invalid native end")
	}
	if fields[0] != uid || strings.TrimSpace(fields[1]) == "" || (fields[6] != "true" && fields[6] != "false") {
		return nil, fmt.Errorf("invalid native event identity or fields")
	}
	location, err := time.LoadLocation(fields[12])
	if err != nil || ambiguousNativeDate(time.Unix(start, 0), location) || ambiguousNativeDate(time.Unix(end, 0), location) {
		return nil, fmt.Errorf("native event dates have an unknown timezone or ambiguous DST occurrence")
	}
	sequence, err := strconv.Atoi(fields[10])
	if err != nil {
		return nil, fmt.Errorf("invalid native sequence")
	}
	updatedUnix, err := strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid native modification date")
	}
	event := &contract.Event{ID: fmt.Sprintf("%s@%d", fields[0], start-cocoaEpochOffset), CalendarID: fields[1], CalendarName: fields[2], Title: fields[3], Start: time.Unix(start, 0), End: time.Unix(end, 0), AllDay: fields[6] == "true", Location: fields[7], Notes: fields[8], URL: fields[9], Sequence: sequence, UpdatedAt: time.Unix(updatedUnix, 0)}
	if in.Start == nil && scope == ScopeThis && start != occurrence+cocoaEpochOffset {
		return nil, fmt.Errorf("native result is not the requested occurrence")
	}
	if (in.Start != nil && start != in.Start.Unix()) || (in.End != nil && end != in.End.Unix()) ||
		(in.Title != nil && event.Title != *in.Title) || (in.Location != nil && event.Location != *in.Location) ||
		(in.Notes != nil && event.Notes != *in.Notes) || (in.URL != nil && event.URL != *in.URL) ||
		(in.AllDay != nil && event.AllDay != *in.AllDay) {
		return nil, fmt.Errorf("native result does not match requested update")
	}
	return event, nil
}

// AppleScript dates lose which instant was intended during a repeated local
// clock interval. Reject both sides of a fold instead of certifying one by guess.
func ambiguousNativeDate(instant time.Time, location *time.Location) bool {
	local := instant.In(location)
	_, offset := local.Zone()
	before, after := local.ZoneBounds()
	var adjacent []time.Time
	if !before.IsZero() {
		adjacent = append(adjacent, before.Add(-time.Second))
	}
	if !after.IsZero() {
		adjacent = append(adjacent, after)
	}
	for _, boundary := range adjacent {
		_, otherOffset := boundary.In(location).Zone()
		if otherOffset == offset {
			continue
		}
		other := instant.Add(time.Duration(offset-otherOffset) * time.Second).In(location)
		if local.Format("2006-01-02 15:04:05") == other.Format("2006-01-02 15:04:05") {
			return true
		}
	}
	return false
}
