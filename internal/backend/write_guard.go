package backend

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// WriteRejectedError reports a decision made before native mutation starts.
// It must not be presented as an uncertain write or automatically retried.
type WriteRejectedError struct{ Reason string }

func (e *WriteRejectedError) Error() string {
	switch e.Reason {
	case "recurring":
		return "recurring-event writes are unsupported in this release; no event was changed"
	case "permission":
		return "Full Calendar Access is required before writing; no event was changed"
	case "legacy_history":
		return "this history entry has no recurrence classification; recreating it is unsupported; no event was changed"
	default:
		return "event identity or recurrence could not be verified; no event was changed"
	}
}

const writeRejectedPrefix = "ACAL_WRITE_REJECTED:"

func decodeWriteRejection(out string) error {
	value := strings.TrimSpace(out)
	if strings.HasPrefix(value, writeRejectedPrefix) {
		return &WriteRejectedError{Reason: strings.TrimPrefix(value, writeRejectedPrefix)}
	}
	return nil
}

// CheckEventWrite allows commands to reject unsupported targets before their
// ordinary event lookup, which can itself miss generated recurring occurrences.
func (b *OsaScriptBackend) CheckEventWrite(ctx context.Context, id string) error {
	uid, occurrence := parseEventID(id)
	if strings.TrimSpace(uid) == "" {
		return &WriteRejectedError{Reason: "unclassified"}
	}
	seconds := int64(0)
	if occurrence > 0 {
		seconds = occurrence + cocoaEpochOffset
	}
	lines := append(appleScriptDateHandlers(), writeGuardScriptHandlers()...)
	lines = append(lines, `on run argv`, `return my independentWriteCheck(item 1 of argv, item 2 of argv as real)`, `end run`)
	out, err := runWriteAppleScript(ctx, lines, uid, strconv.FormatInt(seconds, 10))
	if err != nil {
		return fmt.Errorf("%w: %v", &WriteRejectedError{Reason: "unclassified"}, err)
	}
	if rejection := decodeWriteRejection(out); rejection != nil {
		return rejection
	}
	if strings.TrimSpace(out) != "" {
		return &WriteRejectedError{Reason: "unclassified"}
	}
	return nil
}

func writeGuardScriptHandlers() []string {
	return []string{
		`use framework "EventKit"`,
		`on writeAccessCheck(statusValue)`,
		`if statusValue is not 3 then return "ACAL_WRITE_REJECTED:permission"`,
		`return ""`,
		`end writeAccessCheck`,
		`on independentWriteCheck(uidText, startSeconds)`,
		`set accessResult to my writeAccessCheck((current application's EKEventStore's authorizationStatusForEntityType:0) as integer)`,
		`if accessResult is not "" then return accessResult`,
		`try`,
		`set store to current application's EKEventStore's alloc()'s init()`,
		`set candidates to {}`,
		`set seenIDs to {}`,
		`set directItem to store's calendarItemWithIdentifier:uidText`,
		`if directItem is not missing value then`,
		`set end of candidates to directItem`,
		`set end of seenIDs to directItem's calendarItemIdentifier() as text`,
		`end if`,
		`repeat with entry in (store's calendarItemsWithExternalIdentifier:uidText)`,
		`set candidate to contents of entry`,
		`set candidateID to candidate's calendarItemIdentifier() as text`,
		`if candidateID is not in seenIDs then`,
		`set end of candidates to candidate`,
		`set end of seenIDs to candidateID`,
		`end if`,
		`end repeat`,
		`if (count of candidates) is not 1 then return "ACAL_WRITE_REJECTED:unclassified"`,
		`set candidate to item 1 of candidates`,
		`if (candidate's hasRecurrenceRules() as boolean) or (candidate's isDetached() as boolean) then return "ACAL_WRITE_REJECTED:recurring"`,
		`set actualStart to candidate's startDate()`,
		`if startSeconds > 0 and (actualStart's timeIntervalSince1970() as real) is not startSeconds then return "ACAL_WRITE_REJECTED:unclassified"`,
		`return ""`,
		`on error`,
		`return "ACAL_WRITE_REJECTED:unclassified"`,
		`end try`,
		`end independentWriteCheck`,
	}
}
