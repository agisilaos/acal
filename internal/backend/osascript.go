package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agis/acal/internal/contract"
	_ "modernc.org/sqlite"
)

const cocoaEpochOffset = int64(978307200)

type OsaScriptBackend struct{}

func NewOsaScriptBackend() *OsaScriptBackend { return &OsaScriptBackend{} }

var calendarReadDBCache sync.Map

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
	if err := checkCalendarDBReadable(ctx, dbPath); err != nil {
		msg := err.Error()
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
	return b.listEventsFromDB(ctx, dbPath, f)
}

func (b *OsaScriptBackend) listEventsFromDB(ctx context.Context, dbPath string, f EventFilter) ([]contract.Event, error) {
	fromCocoa := f.From.Unix() - cocoaEpochOffset
	toCocoa := f.To.Unix() - cocoaEpochOffset
	if toCocoa < fromCocoa {
		return nil, fmt.Errorf("invalid time range")
	}

	query := buildListEventsQuery(fromCocoa, toCocoa, f)

	items, err := listEventsViaSQLite(ctx, dbPath, query, f.Limit)
	if err != nil {
		if !shouldFallbackFromSQLite(err) {
			return nil, err
		}
		msg := err.Error()
		items, fbErr := b.listEventsViaAppleScript(ctx, f)
		if fbErr == nil {
			return items, nil
		}
		if isDBAccessDenied(msg) {
			return nil, fmt.Errorf("sqlite query failed: %s (AppleScript fallback failed: %v)", msg, fbErr)
		}
		return nil, fmt.Errorf("sqlite query failed: %s (fallback failed: %v)", msg, fbErr)
	}

	return items, nil
}

func buildListEventsQuery(fromCocoa, toCocoa int64, f EventFilter) string {
	rangeClause := fmt.Sprintf("oc.occurrence_start_date >= %d AND oc.occurrence_start_date <= %d", fromCocoa, toCocoa)
	if f.Overlap {
		startClause := fmt.Sprintf("oc.occurrence_start_date < %d", toCocoa)
		endClause := fmt.Sprintf("oc.occurrence_end_date > %d", fromCocoa)
		validRange := fromCocoa < toCocoa
		if !f.From.IsZero() && !f.To.IsZero() {
			// Subtract the whole-second bound before comparing its remainder:
			// adding nanoseconds to a modern Cocoa timestamp loses float precision.
			if ns := f.To.Nanosecond(); ns != 0 {
				startClause = fmt.Sprintf("(oc.occurrence_start_date - %d) < 0.%09d", toCocoa, ns)
			}
			if ns := f.From.Nanosecond(); ns != 0 {
				endClause = fmt.Sprintf("(oc.occurrence_end_date - %d) > 0.%09d", fromCocoa, ns)
			}
			validRange = f.From.Before(f.To)
		}
		rangeClause = "1=0"
		if validRange {
			rangeClause = startClause + " AND " + endClause
		}
	}
	limitClause := ""
	if f.Limit > 0 {
		limitClause = fmt.Sprintf("\nLIMIT %d", f.Limit)
	}
	calendarClause := ""
	if len(f.Calendars) > 0 {
		calVals := make([]string, 0, len(f.Calendars))
		for _, c := range f.Calendars {
			v := strings.ToLower(strings.TrimSpace(c))
			if v == "" {
				continue
			}
			calVals = append(calVals, sqlQuote(v))
		}
		if len(calVals) > 0 {
			in := strings.Join(calVals, ",")
			calendarClause = fmt.Sprintf("\n  AND (lower(COALESCE(c.UUID, CAST(c.ROWID AS TEXT))) IN (%s) OR lower(COALESCE(c.title, '')) IN (%s))", in, in)
		}
	}
	queryClause := ""
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		p := sqlLikeLiteral(q)
		switch strings.ToLower(strings.TrimSpace(f.Field)) {
		case "", "all":
			queryClause = fmt.Sprintf("\n  AND (lower(COALESCE(ci.summary, '')) LIKE %s ESCAPE '\\' OR lower(COALESCE(l.title, '')) LIKE %s ESCAPE '\\' OR lower(COALESCE(ci.description, '')) LIKE %s ESCAPE '\\')", p, p, p)
		case "title":
			queryClause = fmt.Sprintf("\n  AND lower(COALESCE(ci.summary, '')) LIKE %s ESCAPE '\\'", p)
		case "location":
			queryClause = fmt.Sprintf("\n  AND lower(COALESCE(l.title, '')) LIKE %s ESCAPE '\\'", p)
		case "notes":
			queryClause = fmt.Sprintf("\n  AND lower(COALESCE(ci.description, '')) LIKE %s ESCAPE '\\'", p)
		default:
			queryClause = "\n  AND 1=0"
		}
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
  AND %s
%s%s
ORDER BY oc.occurrence_start_date ASC%s;
`, cocoaEpochOffset, cocoaEpochOffset, cocoaEpochOffset, rangeClause, calendarClause, queryClause, limitClause)
}

func sqlQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func sqlLikeLiteral(v string) string {
	s := strings.ReplaceAll(v, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return sqlQuote("%" + s + "%")
}

func checkCalendarDBReadable(ctx context.Context, dbPath string) error {
	db, err := openCalendarReadDB(dbPath)
	if err != nil {
		return err
	}
	var one int
	return db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

func listEventsViaSQLite(ctx context.Context, dbPath, query string, expectedRows int) ([]contract.Event, error) {
	db, err := openCalendarReadDB(dbPath)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]contract.Event, 0, initialEventCapacity(expectedRows))
	for rows.Next() {
		var id, calID, calName, title, location, notes, url string
		var startUnix, endUnix, allDayRaw, seq, updatedUnix int64
		if err := rows.Scan(&id, &calID, &calName, &title, &startUnix, &endUnix, &allDayRaw, &location, &notes, &url, &seq, &updatedUnix); err != nil {
			return nil, err
		}
		items = append(items, contract.Event{
			ID:           trimIfEdgeSpace(id),
			CalendarID:   trimIfEdgeSpace(calID),
			CalendarName: trimIfEdgeSpace(calName),
			Title:        trimIfEdgeSpace(title),
			Start:        time.Unix(startUnix, 0),
			End:          time.Unix(endUnix, 0),
			AllDay:       allDayRaw == 1,
			Location:     trimIfEdgeSpace(location),
			Notes:        trimIfEdgeSpace(notes),
			URL:          trimIfEdgeSpace(url),
			Sequence:     int(seq),
			UpdatedAt:    time.Unix(updatedUnix, 0),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func initialEventCapacity(expectedRows int) int {
	switch {
	case expectedRows <= 0:
		return 64
	case expectedRows > 2048:
		return 2048
	default:
		return expectedRows
	}
}

func openCalendarReadDB(dbPath string) (*sql.DB, error) {
	dsn := calendarSQLiteDSN(dbPath)
	if v, ok := calendarReadDBCache.Load(dsn); ok {
		return v.(*sql.DB), nil
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if existing, loaded := calendarReadDBCache.LoadOrStore(dsn, db); loaded {
		_ = db.Close()
		return existing.(*sql.DB), nil
	}
	return db, nil
}

func calendarSQLiteDSN(dbPath string) string {
	return "file:" + dbPath + "?mode=ro&immutable=1"
}

func shouldFallbackFromSQLite(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (b *OsaScriptBackend) listEventsViaAppleScript(ctx context.Context, f EventFilter) ([]contract.Event, error) {
	if f.Overlap && !f.From.Before(f.To) {
		return nil, nil
	}
	rangePredicate := "start date >= fromDate and start date <= toDate"
	if f.Overlap {
		rangePredicate = "start date < toDate and end date > fromDate"
	}
	fromUnix := strconv.FormatInt(f.From.Unix(), 10)
	toSecond := f.To.Unix()
	if f.Overlap && f.To.Nanosecond() != 0 {
		// AppleScript dates have second precision. Fetch a superset, then
		// apply the exact overlap bounds before sorting and limiting results.
		toSecond++
	}
	toUnix := strconv.FormatInt(toSecond, 10)
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
		`repeat with e in (every event of c whose ` + rangePredicate + `)`,
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
		if f.Overlap && (!start.Before(f.To) || !end.After(f.From)) {
			continue
		}
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
	return getEventByID(ctx, id, b.ListEvents)
}

func getEventByID(ctx context.Context, id string, listEvents func(context.Context, EventFilter) ([]contract.Event, error)) (*contract.Event, error) {
	_, occurrence, present, valid := parseReadEventID(id)
	if !present || !valid || occurrence == math.MinInt64 || occurrence >= math.MaxInt64-cocoaEpochOffset {
		return nil, errors.New("event not found")
	}
	// SQLite casts fractional starts toward zero: n can represent [n,n+1),
	// (n-1,n], or (-1,1) for positive, negative, or zero n respectively.
	// Include both boundaries; exact ID equality below rejects adjacent IDs.
	f := EventFilter{
		From: time.Unix(occurrence+cocoaEpochOffset-1, 0),
		To:   time.Unix(occurrence+cocoaEpochOffset+1, 0),
	}
	if f.From.IsZero() {
		// ListEvents reserves the Go zero time for an unspecified bound.
		f.From = f.From.Add(-time.Second)
	}
	if f.To.IsZero() {
		f.To = f.To.Add(time.Second)
	}
	items, err := listEvents(ctx, f)
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
