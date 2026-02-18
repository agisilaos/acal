package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseEventID(t *testing.T) {
	uid, occ := parseEventID("ABC-123@792417600")
	if uid != "ABC-123" {
		t.Fatalf("uid mismatch: %q", uid)
	}
	if occ != 792417600 {
		t.Fatalf("occ mismatch: %d", occ)
	}

	uid2, occ2 := parseEventID("ABC-123")
	if uid2 != "ABC-123" || occ2 != 0 {
		t.Fatalf("unexpected parse for uid-only: uid=%q occ=%d", uid2, occ2)
	}
}

func TestTrimOuterQuotes(t *testing.T) {
	if got := trimOuterQuotes("\"hello\""); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := trimOuterQuotes("hello"); got != "hello" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestSplitLines(t *testing.T) {
	lines := splitLines("\"a\nb\"")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("unexpected lines: %+v", lines)
	}
}

func TestBoolToScript(t *testing.T) {
	if boolToScript(true) != "true" {
		t.Fatalf("expected true")
	}
	if boolToScript(false) != "false" {
		t.Fatalf("expected false")
	}
}

func TestContainsFold(t *testing.T) {
	if !containsFold([]string{"Work", "Personal"}, "work") {
		t.Fatalf("expected case-insensitive match")
	}
	if containsFold([]string{"Work"}, "Gym") {
		t.Fatalf("did not expect match")
	}
}

func TestIsDBAccessDenied(t *testing.T) {
	cases := []string{
		`Error: unable to open database "...": authorization denied`,
		"operation not permitted",
		"permission denied",
	}
	for _, tc := range cases {
		if !isDBAccessDenied(tc) {
			t.Fatalf("expected true for %q", tc)
		}
	}
	if isDBAccessDenied("no such table: foo") {
		t.Fatalf("expected false for unrelated sqlite error")
	}
}

func TestResolveRecurrenceScopeAuto(t *testing.T) {
	got, err := resolveRecurrenceScope(ScopeAuto, 792417600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ScopeThis {
		t.Fatalf("got=%q want=%q", got, ScopeThis)
	}

	got, err = resolveRecurrenceScope(ScopeAuto, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ScopeSeries {
		t.Fatalf("got=%q want=%q", got, ScopeSeries)
	}
}

func TestResolveRecurrenceScopeValidation(t *testing.T) {
	got, err := resolveRecurrenceScope(ScopeFuture, 792417600)
	if err != nil {
		t.Fatalf("unexpected error for future with occurrence: %v", err)
	}
	if got != ScopeFuture {
		t.Fatalf("got=%q want=%q", got, ScopeFuture)
	}

	if _, err := resolveRecurrenceScope(ScopeThis, 0); err == nil {
		t.Fatalf("expected error for this without occurrence")
	}
	if _, err := resolveRecurrenceScope(ScopeFuture, 0); err == nil {
		t.Fatalf("expected error for future without occurrence")
	}
	if _, err := resolveRecurrenceScope(RecurrenceScope("bad"), 1); err == nil {
		t.Fatalf("expected error for invalid scope")
	}
}

func TestIsTransientAppleScriptError(t *testing.T) {
	if !isTransientAppleScriptError("AppleEvent timed out. (-1712)") {
		t.Fatalf("expected timeout to be transient")
	}
	if !isTransientAppleScriptError("connection is invalid") {
		t.Fatalf("expected connection issue to be transient")
	}
	if isTransientAppleScriptError("authorization denied") {
		t.Fatalf("expected permission denial to be non-transient")
	}
}

func TestTrimIfEdgeSpace(t *testing.T) {
	in := "calendar-id"
	if got := trimIfEdgeSpace(in); got != in {
		t.Fatalf("expected unchanged string, got: %q", got)
	}
}

func TestTrimIfEdgeSpaceTrimsWhenNeeded(t *testing.T) {
	if got := trimIfEdgeSpace("  hello\t"); got != "hello" {
		t.Fatalf("expected trimmed value, got: %q", got)
	}
}

func TestOsaScriptRetryPolicyFromEnv(t *testing.T) {
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "2")
	t.Setenv("ACAL_OSASCRIPT_RETRY_BACKOFF", "150ms")
	retries, backoff := osascriptRetryPolicy()
	if retries != 2 {
		t.Fatalf("retries mismatch: got=%d want=2", retries)
	}
	if backoff != 150000000 {
		t.Fatalf("backoff mismatch: got=%s want=150ms", backoff)
	}
}

func TestBuildListEventsQueryLimitClause(t *testing.T) {
	q := buildListEventsQuery(1, 2, EventFilter{Limit: 25})
	if !strings.Contains(q, "LIMIT 25") {
		t.Fatalf("expected LIMIT clause in query, got: %s", q)
	}
}

func TestBuildListEventsQueryNoLimitClause(t *testing.T) {
	q := buildListEventsQuery(1, 2, EventFilter{})
	if strings.Contains(q, "LIMIT ") {
		t.Fatalf("did not expect LIMIT clause in query, got: %s", q)
	}
}

func TestBuildListEventsQueryPushesCalendarPredicate(t *testing.T) {
	q := buildListEventsQuery(1, 2, EventFilter{Calendars: []string{"Work", "cal-1"}})
	if !strings.Contains(q, "lower(COALESCE(c.UUID") || !strings.Contains(q, "IN ('work','cal-1')") {
		t.Fatalf("expected calendar pushdown, got: %s", q)
	}
}

func TestBuildListEventsQueryPushesQueryPredicate(t *testing.T) {
	q := buildListEventsQuery(1, 2, EventFilter{Query: "Standup", Field: "title"})
	if !strings.Contains(q, "lower(COALESCE(ci.summary, '')) LIKE") {
		t.Fatalf("expected title LIKE pushdown, got: %s", q)
	}
	if strings.Contains(q, "1=0") {
		t.Fatalf("did not expect impossible predicate for known field, got: %s", q)
	}
}

func TestBuildListEventsQueryUnknownFieldUsesNoResultsPredicate(t *testing.T) {
	q := buildListEventsQuery(1, 2, EventFilter{Query: "x", Field: "bogus"})
	if !strings.Contains(q, "AND 1=0") {
		t.Fatalf("expected impossible predicate for unknown field, got: %s", q)
	}
}

func TestInitialEventCapacity(t *testing.T) {
	if got := initialEventCapacity(0); got != 64 {
		t.Fatalf("unexpected default cap: got=%d want=64", got)
	}
	if got := initialEventCapacity(10); got != 10 {
		t.Fatalf("unexpected explicit cap: got=%d want=10", got)
	}
	if got := initialEventCapacity(5000); got != 2048 {
		t.Fatalf("unexpected capped value: got=%d want=2048", got)
	}
}

func TestCalendarSQLiteDSNUsesReadOnlyImmutableMode(t *testing.T) {
	dsn := calendarSQLiteDSN("/tmp/Calendar.sqlitedb")
	if !strings.Contains(dsn, "mode=ro") {
		t.Fatalf("expected read-only mode in dsn, got: %s", dsn)
	}
	if !strings.Contains(dsn, "immutable=1") {
		t.Fatalf("expected immutable mode in dsn, got: %s", dsn)
	}
}

func TestShouldFallbackFromSQLite(t *testing.T) {
	if !shouldFallbackFromSQLite(errors.New("permission denied")) {
		t.Fatalf("expected fallback for non-context sqlite errors")
	}
	if shouldFallbackFromSQLite(context.Canceled) {
		t.Fatalf("did not expect fallback for canceled context")
	}
	if shouldFallbackFromSQLite(context.DeadlineExceeded) {
		t.Fatalf("did not expect fallback for deadline exceeded context")
	}
	if shouldFallbackFromSQLite(nil) {
		t.Fatalf("did not expect fallback for nil error")
	}
}
