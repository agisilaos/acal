package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

const cocoaEpochOffset = int64(978307200)

type OsaScriptBackend struct{}

func NewOsaScriptBackend() *OsaScriptBackend { return &OsaScriptBackend{} }

func (b *OsaScriptBackend) Doctor(_ context.Context) ([]contract.DoctorCheck, error) {
	checks := []contract.DoctorCheck{}
	if _, err := exec.LookPath("osascript"); err != nil {
		checks = append(checks, contract.DoctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found in PATH"})
		return checks, fmt.Errorf("osascript not found")
	}
	checks = append(checks, contract.DoctorCheck{Name: "osascript", Status: "ok", Message: "osascript found"})

	_, err := runAppleScript([]string{
		`tell application "Calendar"`,
		`return "ok"`,
		`end tell`,
	})
	if err != nil {
		checks = append(checks, contract.DoctorCheck{Name: "calendar_access", Status: "fail", Message: err.Error()})
		return checks, err
	}
	checks = append(checks, contract.DoctorCheck{Name: "calendar_access", Status: "ok", Message: "Calendar automation reachable"})

	if _, err := findCalendarDB(); err != nil {
		checks = append(checks, contract.DoctorCheck{Name: "calendar_db", Status: "fail", Message: err.Error()})
		return checks, err
	}
	dbPath, _ := findCalendarDB()
	if out, err := exec.Command("sqlite3", dbPath, "SELECT 1;").CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		checks = append(checks, contract.DoctorCheck{Name: "calendar_db_read", Status: "fail", Message: msg})
		return checks, fmt.Errorf("calendar database exists but is not readable: %s", msg)
	}
	checks = append(checks, contract.DoctorCheck{Name: "calendar_db", Status: "ok", Message: "Calendar database found"})
	checks = append(checks, contract.DoctorCheck{Name: "calendar_db_read", Status: "ok", Message: "Calendar database readable"})
	return checks, nil
}

func (b *OsaScriptBackend) ListCalendars(_ context.Context) ([]contract.Calendar, error) {
	out, err := runAppleScript([]string{
		`set rows to {}`,
		`tell application "Calendar"`,
		`repeat with c in calendars`,
		`set calID to ""`,
		`try`,
		`set calID to (calendarIdentifier of c as text)`,
		`on error`,
		`set calID to (name of c as text)`,
		`end try`,
		`set rowText to calID & tab & (name of c as text) & tab & (writable of c as text)`,
		`copy rowText to end of rows`,
		`end repeat`,
		`end tell`,
		`set AppleScript's text item delimiters to linefeed`,
		`set joined to rows as text`,
		`set AppleScript's text item delimiters to ""`,
		`return joined`,
	})
	if err != nil {
		return nil, err
	}

	lines := splitLines(out)
	items := make([]contract.Calendar, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		items = append(items, contract.Calendar{
			ID:       strings.TrimSpace(parts[0]),
			Name:     strings.TrimSpace(parts[1]),
			Writable: strings.EqualFold(strings.TrimSpace(parts[2]), "true"),
		})
	}
	return items, nil
}

func (b *OsaScriptBackend) ListEvents(_ context.Context, f EventFilter) ([]contract.Event, error) {
	if f.From.IsZero() || f.To.IsZero() {
		return nil, fmt.Errorf("from/to required")
	}
	dbPath, err := findCalendarDB()
	if err != nil {
		return nil, err
	}

	fromCocoa := f.From.Unix() - cocoaEpochOffset
	toCocoa := f.To.Unix() - cocoaEpochOffset
	if toCocoa < fromCocoa {
		return nil, fmt.Errorf("invalid time range")
	}

	query := fmt.Sprintf(`
SELECT
  (COALESCE(ci.unique_identifier, ci.UUID, CAST(ci.ROWID AS TEXT)) || '@' || CAST(oc.occurrence_start_date AS INTEGER)) AS id,
  COALESCE(c.UUID, CAST(c.ROWID AS TEXT)) AS cal_id,
  COALESCE(c.title, '') AS cal_name,
  COALESCE(ci.summary, '') AS title,
  CAST(oc.occurrence_start_date AS INTEGER) + %d AS start_unix,
  CAST(oc.occurrence_end_date AS INTEGER) + %d AS end_unix,
  COALESCE(ci.all_day, 0) AS all_day,
  COALESCE(l.title, '') AS location,
  COALESCE(ci.description, '') AS notes,
  COALESCE(ci.url, '') AS url,
  COALESCE(ci.sequence_num, 0) AS seq,
  CAST(COALESCE(ci.last_modified, 0) AS INTEGER) + %d AS updated_unix
FROM OccurrenceCache oc
JOIN CalendarItem ci ON ci.ROWID = oc.event_id
JOIN Calendar c ON c.ROWID = oc.calendar_id
LEFT JOIN Location l ON l.item_owner_id = ci.ROWID
WHERE oc.next_reminder_date IS NULL
  AND oc.occurrence_start_date >= %d
  AND oc.occurrence_start_date <= %d
ORDER BY oc.occurrence_start_date ASC;
`, cocoaEpochOffset, cocoaEpochOffset, cocoaEpochOffset, fromCocoa, toCocoa)

	cmd := exec.Command("sqlite3", "-tabs", dbPath, query)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = err.Error()
		}
		items, fbErr := b.listEventsViaAppleScript(f)
		if fbErr == nil {
			return items, nil
		}
		if isDBAccessDenied(msg) {
			return nil, fmt.Errorf("sqlite3 query failed: %s (AppleScript fallback failed: %v)", msg, fbErr)
		}
		return nil, fmt.Errorf("sqlite3 query failed: %s (fallback failed: %v)", msg, fbErr)
	}

	lines := splitLines(string(raw))
	items := make([]contract.Event, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 12 {
			continue
		}
		startUnix, _ := strconv.ParseInt(parts[4], 10, 64)
		endUnix, _ := strconv.ParseInt(parts[5], 10, 64)
		seq, _ := strconv.Atoi(parts[10])
		updatedUnix, _ := strconv.ParseInt(parts[11], 10, 64)
		e := contract.Event{
			ID:           strings.TrimSpace(parts[0]),
			CalendarID:   strings.TrimSpace(parts[1]),
			CalendarName: strings.TrimSpace(parts[2]),
			Title:        strings.TrimSpace(parts[3]),
			Start:        time.Unix(startUnix, 0),
			End:          time.Unix(endUnix, 0),
			AllDay:       strings.TrimSpace(parts[6]) == "1" || strings.EqualFold(strings.TrimSpace(parts[6]), "true"),
			Location:     strings.TrimSpace(parts[7]),
			Notes:        strings.TrimSpace(parts[8]),
			URL:          strings.TrimSpace(parts[9]),
			Sequence:     seq,
			UpdatedAt:    time.Unix(updatedUnix, 0),
		}
		if len(f.Calendars) > 0 && !containsFold(f.Calendars, e.CalendarID) && !containsFold(f.Calendars, e.CalendarName) {
			continue
		}
		if f.Query != "" {
			needle := strings.ToLower(f.Query)
			field := strings.ToLower(f.Field)
			if field == "" || field == "all" {
				if !strings.Contains(strings.ToLower(e.Title), needle) && !strings.Contains(strings.ToLower(e.Location), needle) && !strings.Contains(strings.ToLower(e.Notes), needle) {
					continue
				}
			} else {
				if !strings.Contains(strings.ToLower(selectField(e, field)), needle) {
					continue
				}
			}
		}
		items = append(items, e)
		if f.Limit > 0 && len(items) >= f.Limit {
			break
		}
	}
	return items, nil
}

func (b *OsaScriptBackend) listEventsViaAppleScript(f EventFilter) ([]contract.Event, error) {
	fromUnix := strconv.FormatInt(f.From.Unix(), 10)
	toUnix := strconv.FormatInt(f.To.Unix(), 10)
	out, err := runAppleScript([]string{
		`on cleanText(v)`,
		`set s to v as text`,
		`set AppleScript's text item delimiters to tab`,
		`set parts to text items of s`,
		`set AppleScript's text item delimiters to " "`,
		`set s to parts as text`,
		`set AppleScript's text item delimiters to return`,
		`set parts to text items of s`,
		`set AppleScript's text item delimiters to " "`,
		`set s to parts as text`,
		`set AppleScript's text item delimiters to linefeed`,
		`set parts to text items of s`,
		`set AppleScript's text item delimiters to " "`,
		`set s to parts as text`,
		`set AppleScript's text item delimiters to ""`,
		`return s`,
		`end cleanText`,
		`on run argv`,
		`set fromUnix to item 1 of argv as integer`,
		`set toUnix to item 2 of argv as integer`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`set fromDate to epoch + fromUnix`,
		`set toDate to epoch + toUnix`,
		`set rows to {}`,
		`tell application "Calendar"`,
		`repeat with c in calendars`,
		`set calID to ""`,
		`try`,
		`set calID to (calendarIdentifier of c as text)`,
		`on error`,
		`set calID to (name of c as text)`,
		`end try`,
		`set calName to my cleanText(name of c as text)`,
		`repeat with e in (every event of c)`,
		`set evStartDate to start date of e`,
		`if ((evStartDate is greater than fromDate) or (evStartDate is equal to fromDate)) and ((evStartDate is less than toDate) or (evStartDate is equal to toDate)) then`,
		`set evUID to (uid of e as text)`,
		`set evTitle to my cleanText(summary of e as text)`,
		`set evEndDate to end date of e`,
		`set evStartUnix to ((evStartDate - epoch) as integer)`,
		`set evEndUnix to ((evEndDate - epoch) as integer)`,
		`set evAllDay to (allday event of e as text)`,
		`set evLoc to ""`,
		`try`,
		`set evLoc to my cleanText(location of e as text)`,
		`end try`,
		`set rowText to evUID & tab & calID & tab & calName & tab & evTitle & tab & (evStartUnix as text) & tab & (evEndUnix as text) & tab & evAllDay & tab & evLoc & tab & "" & tab & ""`,
		`copy rowText to end of rows`,
		`end if`,
		`end repeat`,
		`end repeat`,
		`end tell`,
		`set AppleScript's text item delimiters to linefeed`,
		`set joined to rows as text`,
		`set AppleScript's text item delimiters to ""`,
		`return joined`,
		`end run`,
	}, fromUnix, toUnix)
	if err != nil {
		return nil, err
	}
	lines := splitLines(out)
	items := make([]contract.Event, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 10 {
			continue
		}
		startUnix, err := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64)
		if err != nil {
			continue
		}
		endUnix, err := strconv.ParseInt(strings.TrimSpace(parts[5]), 10, 64)
		if err != nil {
			continue
		}
		start := time.Unix(startUnix, 0).In(f.From.Location())
		end := time.Unix(endUnix, 0).In(f.From.Location())
		startCocoa := start.Unix() - cocoaEpochOffset
		e := contract.Event{
			ID:           fmt.Sprintf("%s@%d", strings.TrimSpace(parts[0]), startCocoa),
			CalendarID:   strings.TrimSpace(parts[1]),
			CalendarName: strings.TrimSpace(parts[2]),
			Title:        strings.TrimSpace(parts[3]),
			Start:        start,
			End:          end,
			AllDay:       strings.EqualFold(strings.TrimSpace(parts[6]), "true"),
			Location:     strings.TrimSpace(parts[7]),
			Notes:        strings.TrimSpace(parts[8]),
			URL:          strings.TrimSpace(parts[9]),
			Sequence:     0,
			UpdatedAt:    time.Time{},
		}
		if len(f.Calendars) > 0 && !containsFold(f.Calendars, e.CalendarID) && !containsFold(f.Calendars, e.CalendarName) {
			continue
		}
		if f.Query != "" {
			needle := strings.ToLower(f.Query)
			field := strings.ToLower(f.Field)
			if field == "" || field == "all" {
				if !strings.Contains(strings.ToLower(e.Title), needle) && !strings.Contains(strings.ToLower(e.Location), needle) && !strings.Contains(strings.ToLower(e.Notes), needle) {
					continue
				}
			} else {
				if !strings.Contains(strings.ToLower(selectField(e, field)), needle) {
					continue
				}
			}
		}
		items = append(items, e)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Start.Equal(items[j].Start) {
			return items[i].ID < items[j].ID
		}
		return items[i].Start.Before(items[j].Start)
	})
	if f.Limit > 0 && len(items) > f.Limit {
		items = items[:f.Limit]
	}
	return items, nil
}

func (b *OsaScriptBackend) GetEventByID(ctx context.Context, id string) (*contract.Event, error) {
	now := time.Now()
	items, err := b.ListEvents(ctx, EventFilter{From: now.AddDate(-3, 0, 0), To: now.AddDate(3, 0, 0), Limit: 0})
	if err != nil {
		return nil, err
	}
	for _, e := range items {
		if e.ID == id {
			cp := e
			return &cp, nil
		}
	}
	return nil, errors.New("event not found")
}

func (b *OsaScriptBackend) AddEvent(ctx context.Context, in EventCreateInput) (*contract.Event, error) {
	if strings.TrimSpace(in.Calendar) == "" {
		return nil, fmt.Errorf("calendar is required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	if in.End.Before(in.Start) || in.End.Equal(in.Start) {
		return nil, fmt.Errorf("end must be after start")
	}
	args := []string{
		in.Calendar,
		in.Title,
		strconv.FormatInt(in.Start.Unix(), 10),
		strconv.FormatInt(in.End.Unix(), 10),
		in.Location,
		in.Notes,
		in.URL,
		boolToScript(in.AllDay),
	}
	out, err := runAppleScript([]string{
		`on run argv`,
		`set calName to item 1 of argv`,
		`set titleText to item 2 of argv`,
		`set startUnix to item 3 of argv as integer`,
		`set endUnix to item 4 of argv as integer`,
		`set locText to item 5 of argv`,
		`set notesText to item 6 of argv`,
		`set urlText to item 7 of argv`,
		`set allDayText to item 8 of argv`,
		`set allDayVal to false`,
		`if allDayText is "true" then set allDayVal to true`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`set startDate to epoch + startUnix`,
		`set endDate to epoch + endUnix`,
		`tell application "Calendar"`,
		`set targetCal to missing value`,
		`repeat with c in calendars`,
		`if (name of c as text) is calName then set targetCal to c`,
		`if targetCal is missing value then`,
		`try`,
		`if (calendarIdentifier of c as text) is calName then set targetCal to c`,
		`end try`,
		`end if`,
		`if targetCal is not missing value then exit repeat`,
		`end repeat`,
		`if targetCal is missing value then error "calendar not found"`,
		`set newEvent to make new event at end of events of targetCal with properties {summary:titleText, start date:startDate, end date:endDate, location:locText, description:notesText, url:urlText, allday event:allDayVal}`,
		`return uid of newEvent as text`,
		`end tell`,
		`end run`,
	}, args...)
	if err != nil {
		return nil, err
	}
	uid := strings.TrimSpace(trimOuterQuotes(strings.TrimSpace(out)))
	if uid == "" {
		return nil, fmt.Errorf("failed to create event")
	}
	item, ferr := b.findByUID(ctx, uid, in.Start, in.End)
	if ferr == nil {
		return item, nil
	}
	// OccurrenceCache can lag immediately after writes; return a deterministic ID anyway.
	return &contract.Event{
		ID:           fmt.Sprintf("%s@%d", uid, in.Start.Unix()-cocoaEpochOffset),
		CalendarID:   in.Calendar,
		CalendarName: in.Calendar,
		Title:        in.Title,
		Start:        in.Start,
		End:          in.End,
		AllDay:       in.AllDay,
		Location:     in.Location,
		Notes:        in.Notes,
		URL:          in.URL,
	}, nil
}

func (b *OsaScriptBackend) UpdateEvent(ctx context.Context, id string, in EventUpdateInput) (*contract.Event, error) {
	uid, occ := parseEventID(id)
	if uid == "" {
		return nil, fmt.Errorf("invalid event id")
	}
	scope, err := resolveRecurrenceScope(in.Scope, occ)
	if err != nil {
		return nil, err
	}
	if scope == ScopeFuture {
		return nil, fmt.Errorf("recurrence scope %q is not supported by osascript backend yet", ScopeFuture)
	}

	keep := "__ACAL_KEEP__"
	allDay := keep
	if in.AllDay != nil {
		allDay = boolToScript(*in.AllDay)
	}
	start := keep
	if in.Start != nil {
		start = strconv.FormatInt(in.Start.Unix(), 10)
	}
	end := keep
	if in.End != nil {
		end = strconv.FormatInt(in.End.Unix(), 10)
	}
	title := keep
	if in.Title != nil {
		title = *in.Title
	}
	location := keep
	if in.Location != nil {
		location = *in.Location
	}
	notes := keep
	if in.Notes != nil {
		notes = *in.Notes
	}
	url := keep
	if in.URL != nil {
		url = *in.URL
	}
	occUnix := "0"
	if occ > 0 {
		occUnix = strconv.FormatInt(occ+cocoaEpochOffset, 10)
	}

	out, err := runAppleScript([]string{
		`on run argv`,
		`set uidText to item 1 of argv`,
		`set scopeText to item 2 of argv`,
		`set occUnix to item 3 of argv as integer`,
		`set titleText to item 4 of argv`,
		`set startText to item 5 of argv`,
		`set endText to item 6 of argv`,
		`set locText to item 7 of argv`,
		`set notesText to item 8 of argv`,
		`set urlText to item 9 of argv`,
		`set allDayText to item 10 of argv`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`tell application "Calendar"`,
		`set targetEvent to missing value`,
		`repeat with c in calendars`,
		`if scopeText is "series" then`,
		`try`,
		`set targetEvent to first event of c whose uid is uidText`,
		`exit repeat`,
		`on error`,
		`set targetEvent to missing value`,
		`end try`,
		`else`,
		`try`,
		`set targetEvent to first event of c whose uid is uidText and ((start date of it - epoch) as integer) is occUnix`,
		`exit repeat`,
		`on error`,
		`set targetEvent to missing value`,
		`end try`,
		`end if`,
		`end repeat`,
		`if targetEvent is missing value then error "event not found"`,
		`if titleText is not "__ACAL_KEEP__" then set summary of targetEvent to titleText`,
		`if locText is not "__ACAL_KEEP__" then set location of targetEvent to locText`,
		`if notesText is not "__ACAL_KEEP__" then set description of targetEvent to notesText`,
		`if urlText is not "__ACAL_KEEP__" then set url of targetEvent to urlText`,
		`if startText is not "__ACAL_KEEP__" then set start date of targetEvent to (epoch + (startText as integer))`,
		`if endText is not "__ACAL_KEEP__" then set end date of targetEvent to (epoch + (endText as integer))`,
		`if allDayText is not "__ACAL_KEEP__" then`,
		`if allDayText is "true" then`,
		`set allday event of targetEvent to true`,
		`else`,
		`set allday event of targetEvent to false`,
		`end if`,
		`end if`,
		`return uid of targetEvent as text`,
		`end tell`,
		`end run`,
	}, uid, string(scope), occUnix, title, start, end, location, notes, url, allDay)
	if err != nil {
		return nil, err
	}
	updatedUID := strings.TrimSpace(trimOuterQuotes(strings.TrimSpace(out)))
	if updatedUID == "" {
		updatedUID = uid
	}
	// Refresh from read backend using date window around now.
	now := time.Now()
	items, err := b.ListEvents(ctx, EventFilter{From: now.AddDate(-3, 0, 0), To: now.AddDate(3, 0, 0)})
	if err != nil {
		return nil, err
	}
	for _, e := range items {
		idUID, _ := parseEventID(e.ID)
		if idUID == updatedUID {
			cp := e
			return &cp, nil
		}
	}
	fallback := &contract.Event{ID: fmt.Sprintf("%s@%d", updatedUID, now.Unix()-cocoaEpochOffset)}
	if in.Title != nil {
		fallback.Title = *in.Title
	}
	if in.Location != nil {
		fallback.Location = *in.Location
	}
	if in.Notes != nil {
		fallback.Notes = *in.Notes
	}
	if in.URL != nil {
		fallback.URL = *in.URL
	}
	if in.Start != nil {
		fallback.Start = *in.Start
	}
	if in.End != nil {
		fallback.End = *in.End
	}
	if in.AllDay != nil {
		fallback.AllDay = *in.AllDay
	}
	return fallback, nil
}

func (b *OsaScriptBackend) DeleteEvent(_ context.Context, id string, scope RecurrenceScope) error {
	uid, occ := parseEventID(id)
	if uid == "" {
		return fmt.Errorf("invalid event id")
	}
	resolvedScope, err := resolveRecurrenceScope(scope, occ)
	if err != nil {
		return err
	}
	if resolvedScope == ScopeFuture {
		return fmt.Errorf("recurrence scope %q is not supported by osascript backend yet", ScopeFuture)
	}
	occUnix := "0"
	if occ > 0 {
		occUnix = strconv.FormatInt(occ+cocoaEpochOffset, 10)
	}
	_, err = runAppleScript([]string{
		`on run argv`,
		`set uidText to item 1 of argv`,
		`set scopeText to item 2 of argv`,
		`set occUnix to item 3 of argv as integer`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`tell application "Calendar"`,
		`repeat with c in calendars`,
		`if scopeText is "series" then`,
		`try`,
		`set targetEvent to first event of c whose uid is uidText`,
		`delete targetEvent`,
		`return "ok"`,
		`on error`,
		`end try`,
		`else`,
		`try`,
		`set targetEvent to first event of c whose uid is uidText and ((start date of it - epoch) as integer) is occUnix`,
		`delete targetEvent`,
		`return "ok"`,
		`on error`,
		`end try`,
		`end if`,
		`end repeat`,
		`error "event not found"`,
		`end tell`,
		`end run`,
	}, uid, string(resolvedScope), occUnix)
	return err
}

func resolveRecurrenceScope(scope RecurrenceScope, occurrenceCocoa int64) (RecurrenceScope, error) {
	switch scope {
	case "", ScopeAuto:
		if occurrenceCocoa > 0 {
			return ScopeThis, nil
		}
		return ScopeSeries, nil
	case ScopeThis, ScopeFuture:
		if occurrenceCocoa <= 0 {
			return "", fmt.Errorf("scope %q requires an occurrence event id (<uid>@<occurrence>)", scope)
		}
		return scope, nil
	case ScopeSeries:
		return ScopeSeries, nil
	default:
		return "", fmt.Errorf("invalid recurrence scope: %q", scope)
	}
}

func findCalendarDB() (string, error) {
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "Library/Group Containers/group.com.apple.calendar/Calendar.sqlitedb"),
		filepath.Join(os.Getenv("HOME"), "Library/Calendars/Calendar.sqlitedb"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("calendar database not found")
}

func runAppleScript(lines []string, args ...string) (string, error) {
	cmdArgs := []string{"-s", "s"}
	for _, line := range lines {
		cmdArgs = append(cmdArgs, "-e", line)
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("osascript", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript failed: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func parseEventID(id string) (string, int64) {
	parts := strings.Split(strings.TrimSpace(id), "@")
	if len(parts) < 2 {
		return strings.TrimSpace(id), 0
	}
	occ, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return strings.Join(parts[:len(parts)-1], "@"), occ
}

func trimOuterQuotes(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

func boolToScript(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (b *OsaScriptBackend) findByUID(ctx context.Context, uid string, start, end time.Time) (*contract.Event, error) {
	from := start.Add(-24 * time.Hour)
	to := end.Add(24 * time.Hour)
	items, err := b.ListEvents(ctx, EventFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	for _, e := range items {
		idUID, _ := parseEventID(e.ID)
		if idUID == uid {
			cp := e
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("created event not visible in occurrence cache yet")
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") && len(s) >= 2 {
		s = s[1 : len(s)-1]
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func containsFold(items []string, val string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(val)) {
			return true
		}
	}
	return false
}

func selectField(e contract.Event, field string) string {
	switch field {
	case "title":
		return e.Title
	case "location":
		return e.Location
	case "notes":
		return e.Notes
	default:
		return ""
	}
}

func isDBAccessDenied(msg string) bool {
	s := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(s, "authorization denied") ||
		strings.Contains(s, "not authorized") ||
		strings.Contains(s, "operation not permitted") ||
		strings.Contains(s, "permission denied")
}
