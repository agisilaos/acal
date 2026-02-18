package backend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

const cocoaEpochOffset = int64(978307200)

type OsaScriptBackend struct{}

func NewOsaScriptBackend() *OsaScriptBackend { return &OsaScriptBackend{} }

func (b *OsaScriptBackend) Doctor(ctx context.Context) ([]contract.DoctorCheck, error) {
	checks := []contract.DoctorCheck{}
	if _, err := exec.LookPath("osascript"); err != nil {
		checks = append(checks, contract.DoctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found in PATH"})
		return checks, fmt.Errorf("osascript not found")
	}
	checks = append(checks, contract.DoctorCheck{Name: "osascript", Status: "ok", Message: "osascript found"})

	_, err := runAppleScript(ctx, []string{
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
	if out, err := exec.CommandContext(ctx, "sqlite3", dbPath, "SELECT 1;").CombinedOutput(); err != nil {
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

func (b *OsaScriptBackend) ListCalendars(ctx context.Context) ([]contract.Calendar, error) {
	out, err := runAppleScript(ctx, []string{
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

func (b *OsaScriptBackend) ListEvents(ctx context.Context, f EventFilter) ([]contract.Event, error) {
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

	query := buildListEventsQuery(fromCocoa, toCocoa, f.Limit)

	cmd := exec.CommandContext(ctx, "sqlite3", "-tabs", dbPath, query)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = err.Error()
		}
		items, fbErr := b.listEventsViaAppleScript(ctx, f)
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
	}
	return items, nil
}

func buildListEventsQuery(fromCocoa, toCocoa int64, limit int) string {
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("\nLIMIT %d", limit)
	}
	return fmt.Sprintf(`
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
ORDER BY oc.occurrence_start_date ASC%s;
`, cocoaEpochOffset, cocoaEpochOffset, cocoaEpochOffset, fromCocoa, toCocoa, limitClause)
}

func (b *OsaScriptBackend) listEventsViaAppleScript(ctx context.Context, f EventFilter) ([]contract.Event, error) {
	fromUnix := strconv.FormatInt(f.From.Unix(), 10)
	toUnix := strconv.FormatInt(f.To.Unix(), 10)
	out, err := runAppleScript(ctx, []string{
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
		`repeat with e in (every event of c whose start date >= fromDate and start date <= toDate)`,
		`set evStartDate to start date of e`,
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
