package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

// Foundation converts absolute Unix instants without locale-dependent date
// parsing or arithmetic on a local civil-time epoch.
func appleScriptDateHandlers() []string {
	return []string{
		`use framework "Foundation"`,
		`use scripting additions`,
		`on nativeDate(unixValue)`,
		`return (current application's NSDate's dateWithTimeIntervalSince1970:unixValue) as date`,
		`end nativeDate`,
		`on unixSeconds(nativeValue)`,
		`set instant to current application's NSDate's dateWithTimeInterval:0 sinceDate:nativeValue`,
		`set secondsValue to current application's NSNumber's numberWithDouble:(instant's timeIntervalSince1970())`,
		`return secondsValue's stringValue() as text`,
		`end unixSeconds`,
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

func runAppleScript(ctx context.Context, lines []string, args ...string) (string, error) {
	cmdArgs := []string{"-s", "h"}
	for _, line := range lines {
		cmdArgs = append(cmdArgs, "-e", line)
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, args...)
	retries, backoff := osascriptRetryPolicy()
	return runAppleScriptCommand(ctx, cmdArgs, retries, backoff)
}

// Updates must never retry a script that may already have changed Calendar.
func runUpdateAppleScript(ctx context.Context, lines []string, args ...string) (string, error) {
	cmdArgs := []string{"-s", "h"}
	for _, line := range lines {
		cmdArgs = append(cmdArgs, "-e", line)
	}
	cmdArgs = append(cmdArgs, "--")
	return runAppleScriptCommand(ctx, append(cmdArgs, args...), 0, 0)
}

func runAppleScriptCommand(ctx context.Context, cmdArgs []string, retries int, backoff time.Duration) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		cmd := exec.CommandContext(ctx, "osascript", cmdArgs...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return string(out), nil
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		lastErr = fmt.Errorf("osascript failed: %s", msg)
		if attempt == retries || !isTransientAppleScriptError(msg) {
			break
		}
		wait := backoff * time.Duration(1<<attempt)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}
	return "", lastErr
}

func osascriptRetryPolicy() (int, time.Duration) {
	retries := 0
	if v := strings.TrimSpace(os.Getenv("ACAL_OSASCRIPT_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			retries = n
		}
	}
	backoff := 200 * time.Millisecond
	if v := strings.TrimSpace(os.Getenv("ACAL_OSASCRIPT_RETRY_BACKOFF")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			backoff = d
		}
	}
	return retries, backoff
}

func isTransientAppleScriptError(msg string) bool {
	s := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(s, "appleevent timed out") ||
		strings.Contains(s, "(-1712)") ||
		strings.Contains(s, "connection is invalid") ||
		strings.Contains(s, "temporarily unavailable") ||
		strings.Contains(s, "busy") ||
		strings.Contains(s, "calendar got an error: not running")
}

func parseEventID(id string) (string, int64) {
	parts := strings.Split(strings.TrimSpace(id), "@")
	if len(parts) < 2 {
		return strings.TrimSpace(id), 0
	}
	occ, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return strings.Join(parts[:len(parts)-1], "@"), occ
}

// parseReadEventID distinguishes a missing occurrence from an invalid one.
// Keep parseEventID's existing write/reminder semantics separate.
func parseReadEventID(id string) (uid string, occurrence int64, present, valid bool) {
	i := strings.LastIndexByte(id, '@')
	if i < 0 {
		return id, 0, false, false
	}
	uid, suffix := id[:i], id[i+1:]
	occurrence, err := strconv.ParseInt(suffix, 10, 64)
	valid = uid != "" && err == nil && strconv.FormatInt(occurrence, 10) == suffix
	return uid, occurrence, true, valid
}

func trimOuterQuotes(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

// The runner emits raw text (-s h). Preserve tabs, including empty trailing
// cells; trimming whitespace here can silently discard complete event rows.
func splitLines(s string) []string {
	s = strings.Trim(s, "\r\n")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSuffix(p, "\r")
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

func trimIfEdgeSpace(s string) string {
	if s == "" {
		return ""
	}
	if !isEdgeSpaceByte(s[0]) && !isEdgeSpaceByte(s[len(s)-1]) {
		return s
	}
	return strings.TrimSpace(s)
}

func isEdgeSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
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
