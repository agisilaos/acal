package backend

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
)

func (b *OsaScriptBackend) AddEvent(ctx context.Context, in EventCreateInput) (*contract.Event, error) {
	if strings.TrimSpace(in.Calendar) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("calendar and title required")
	}
	if in.Start.IsZero() || in.End.IsZero() || !in.End.After(in.Start) {
		return nil, fmt.Errorf("invalid start/end")
	}

	if strings.TrimSpace(in.RepeatRule) != "" {
		return nil, &WriteRejectedError{Reason: "recurring"}
	}
	allDay := boolToScript(in.AllDay)
	startUnix := strconv.FormatInt(in.Start.Unix(), 10)
	endUnix := strconv.FormatInt(in.End.Unix(), 10)
	repeatText := strings.ToLower(strings.TrimSpace(in.RepeatRule))
	reminderMins, err := nativeReminderMinutes(in.ReminderOffset)
	if err != nil {
		return nil, err
	}
	out, err := runWriteAppleScript(ctx, append(append(appleScriptDateHandlers(), alarmScriptHandlers()...), []string{
		`on run argv`,
		`set calName to item 1 of argv`,
		`set titleText to item 2 of argv`,
		`set startText to item 3 of argv`,
		`set endText to item 4 of argv`,
		`set locationText to item 5 of argv`,
		`set notesText to item 6 of argv`,
		`set urlText to item 7 of argv`,
		`set allDayText to item 8 of argv`,
		`set repeatText to item 9 of argv`,
		`set reminderText to item 10 of argv`,
		`if reminderText is not "__ACAL_KEEP__" and (current application's EKEventStore's authorizationStatusForEntityType:0) as integer is not 3 then return "ACAL_WRITE_REJECTED:permission"`,
		`set startDate to (my nativeDate(startText as integer))`,
		`set endDate to (my nativeDate(endText as integer))`,
		`tell application "Calendar"`,
		`set targetCal to missing value`,
		`try`,
		`set targetCal to first calendar whose name is calName`,
		`on error`,
		`return "ACAL_CREATE_REJECTED:calendar_not_found"`,
		`end try`,
		`set newEvent to make new event at end of events of targetCal with properties {summary:titleText, start date:startDate, end date:endDate}`,
		`if allDayText is "true" then set allday event of newEvent to true`,
		`if locationText is not "" then set location of newEvent to locationText`,
		`if notesText is not "" then set description of newEvent to notesText`,
		`if urlText is not "" then set url of newEvent to urlText`,
		`if repeatText is not "" then`,
		`if repeatText starts with "daily" then set recurrence of newEvent to daily`,
		`if repeatText starts with "weekly" then set recurrence of newEvent to weekly`,
		`if repeatText starts with "monthly" then set recurrence of newEvent to monthly`,
		`if repeatText starts with "yearly" then set recurrence of newEvent to yearly`,
		`end if`,
		`if reminderText is not "__ACAL_KEEP__" then`,
		`my replaceDisplayAlarm(newEvent, targetCal, reminderText, "series")`,
		`end if`,
		`return uid of newEvent as text`,
		`end tell`,
		`end run`,
	}...), in.Calendar, in.Title, startUnix, endUnix, in.Location, in.Notes, in.URL, allDay, repeatText, reminderMins)
	if err != nil {
		var commandErr *appleScriptCommandError
		if errors.As(err, &commandErr) && commandErr.Started {
			return nil, &CreationOutcomeError{Err: err}
		}
		return nil, err
	}
	if strings.TrimSpace(out) == writeRejectedPrefix+"permission" {
		return nil, &WriteRejectedError{Reason: "permission"}
	}
	if strings.TrimSpace(out) == "ACAL_CREATE_REJECTED:calendar_not_found" {
		return nil, fmt.Errorf("calendar not found")
	}
	uid := strings.TrimSpace(trimOuterQuotes(strings.TrimSpace(out)))
	if uid == "" || strings.HasPrefix(uid, writeRejectedPrefix) || strings.HasPrefix(uid, "ACAL_CREATE_REJECTED:") {
		return nil, &CreationOutcomeError{Err: fmt.Errorf("native creation result unavailable or invalid")}
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

func buildUpdateEventScript(uid string, occ int64, scope RecurrenceScope, in EventUpdateInput) ([]string, []string, error) {
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
	// Omitted strings keep their argv slots but emit no setter. Supplied values
	// stay in argv so even empty strings and the legacy sentinel remain literal.
	var title, location, notes, url string
	var stringSetters []string
	if in.Title != nil {
		title = *in.Title
		stringSetters = append(stringSetters, `set summary of targetRef to titleText`)
	}
	if in.Location != nil {
		location = *in.Location
		stringSetters = append(stringSetters, `set location of targetRef to locText`)
	}
	if in.Notes != nil {
		notes = *in.Notes
		stringSetters = append(stringSetters, `set description of targetRef to notesText`)
	}
	if in.URL != nil {
		url = *in.URL
		stringSetters = append(stringSetters, `set url of targetRef to urlText`)
	}
	repeatText := keep
	if in.RepeatRule != nil {
		repeatText = strings.ToLower(strings.TrimSpace(*in.RepeatRule))
	}
	reminderMins, err := nativeReminderMinutes(in.ReminderOffset)
	if err != nil {
		return nil, nil, err
	}
	clearReminder := "false"
	if in.ClearReminder {
		clearReminder = "true"
	}
	occUnix := "0"
	if occ > 0 {
		occUnix = strconv.FormatInt(occ+cocoaEpochOffset, 10)
	}

	lines := append(append(append(updateResultScriptHandlers(), alarmScriptHandlers()...), writeGuardScriptHandlers()...), []string{
		`on run argv`,
		`set uidText to item 1 of argv`,
		`set scopeText to item 2 of argv`,
		`set occUnix to item 3 of argv as integer`,
		`set occurrenceDate to my nativeDate(occUnix)`,
		`set titleText to item 4 of argv`,
		`set startText to item 5 of argv`,
		`set endText to item 6 of argv`,
		`set locText to item 7 of argv`,
		`set notesText to item 8 of argv`,
		`set urlText to item 9 of argv`,
		`set allDayText to item 10 of argv`,
		`set repeatText to item 11 of argv`,
		`set reminderText to item 12 of argv`,
		`set clearReminderText to item 13 of argv`,
		`set rejection to my independentWriteCheck(uidText, occUnix)`,
		`if rejection is not "" then return rejection`,
		`tell application "Calendar"`,
		`set representativeRef to missing value`,
		`set representativeCal to missing value`,
		`set foundAny to false`,
		`repeat with c in calendars`,
		`set targetEvents to {}`,
		`if scopeText is "series" then`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText}`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else if scopeText is "future" then`,
		`try`,
		`set targetEvents to every event of c whose uid is uidText and start date is greater than or equal to occurrenceDate`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText and start date is occurrenceDate}`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`end if`,
		`if (count of targetEvents) > 0 then`,
		`set foundAny to true`,
		`repeat with targetEvent in targetEvents`,
		`set targetRef to contents of targetEvent`,
	}...)
	lines = append(lines, stringSetters...)
	lines = append(lines, []string{
		`if startText is not "__ACAL_KEEP__" then set start date of targetRef to (my nativeDate(startText as integer))`,
		`if endText is not "__ACAL_KEEP__" then set end date of targetRef to (my nativeDate(endText as integer))`,
		`if allDayText is not "__ACAL_KEEP__" then`,
		`if allDayText is "true" then`,
		`set allday event of targetRef to true`,
		`else`,
		`set allday event of targetRef to false`,
		`end if`,
		`end if`,
		`if repeatText is not "__ACAL_KEEP__" then`,
		`if repeatText is "" then`,
		`set recurrence of targetRef to ""`,
		`else`,
		`if repeatText starts with "daily" then set recurrence of targetRef to daily`,
		`if repeatText starts with "weekly" then set recurrence of targetRef to weekly`,
		`if repeatText starts with "monthly" then set recurrence of targetRef to monthly`,
		`if repeatText starts with "yearly" then set recurrence of targetRef to yearly`,
		`end if`,
		`end if`,
		`if clearReminderText is "true" or reminderText is not "__ACAL_KEEP__" then`,
		`my replaceDisplayAlarm(targetRef, c, reminderText, scopeText)`,
		`end if`,
		`if representativeRef is missing value then`,
		`set representativeRef to targetRef`,
		`set representativeCal to c`,
		`end if`,
		`end repeat`,
		`if scopeText is not "future" then exit repeat`,
		`end if`,
		`end repeat`,
		`if foundAny is false then error "event not found"`,
		`try`,
		`set resultFields to {uid of representativeRef as text, name of representativeCal as text, name of representativeCal as text, my optionalText(summary of representativeRef), my unixSeconds(start date of representativeRef), my unixSeconds(end date of representativeRef), (allday event of representativeRef) as text, my optionalText(location of representativeRef), my optionalText(description of representativeRef), my optionalText(url of representativeRef), (sequence of representativeRef) as text, my unixSeconds(stamp date of representativeRef), my nativeTimezone()}`,
		`try`,
		`set item 2 of resultFields to calendarIdentifier of representativeCal as text`,
		`end try`,
		`return my resultJSON(resultFields)`,
		`on error`,
		`return "ACAL_APPLIED_UNVERIFIED"`,
		`end try`,
		`end tell`,
		`end run`,
	}...)
	return lines, []string{uid, string(scope), occUnix, title, start, end, location, notes, url, allDay, repeatText, reminderMins, clearReminder}, nil
}

func (b *OsaScriptBackend) UpdateEvent(ctx context.Context, id string, in EventUpdateInput) (*contract.Event, error) {
	if in.RepeatRule != nil && strings.TrimSpace(*in.RepeatRule) != "" {
		return nil, &WriteRejectedError{Reason: "recurring"}
	}
	uid, occ := parseEventID(id)
	if uid == "" {
		return nil, fmt.Errorf("invalid event id")
	}
	scope, err := resolveRecurrenceScope(in.Scope, occ)
	if err != nil {
		return nil, err
	}

	lines, args, err := buildUpdateEventScript(uid, occ, scope, in)
	if err != nil {
		return nil, err
	}
	out, err := runWriteAppleScript(ctx, lines, args...)
	if err != nil {
		return nil, &UpdateOutcomeError{Err: err}
	}
	if err := decodeWriteRejection(out); err != nil {
		return nil, err
	}
	event, err := decodeUpdateResult(out, uid, occ, scope, in)
	if err != nil {
		return nil, &UpdateOutcomeError{Applied: true, Err: err}
	}
	return event, nil
}

func (b *OsaScriptBackend) DeleteEvent(ctx context.Context, id string, scope RecurrenceScope) error {
	uid, occ := parseEventID(id)
	if uid == "" {
		return fmt.Errorf("invalid event id")
	}
	resolvedScope, err := resolveRecurrenceScope(scope, occ)
	if err != nil {
		return err
	}
	occUnix := "0"
	if occ > 0 {
		occUnix = strconv.FormatInt(occ+cocoaEpochOffset, 10)
	}
	out, err := runWriteAppleScript(ctx, append(append(appleScriptDateHandlers(), writeGuardScriptHandlers()...), []string{
		`on run argv`,
		`set uidText to item 1 of argv`,
		`set scopeText to item 2 of argv`,
		`set occUnix to item 3 of argv as integer`,
		`set occurrenceDate to my nativeDate(occUnix)`,
		`set rejection to my independentWriteCheck(uidText, occUnix)`,
		`if rejection is not "" then return rejection`,
		`tell application "Calendar"`,
		`set foundAny to false`,
		`repeat with c in calendars`,
		`set targetEvents to {}`,
		`if scopeText is "series" then`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText}`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else if scopeText is "future" then`,
		`try`,
		`set targetEvents to every event of c whose uid is uidText and start date is greater than or equal to occurrenceDate`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText and start date is occurrenceDate}`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`end if`,
		`if (count of targetEvents) > 0 then`,
		`set foundAny to true`,
		`repeat with targetEvent in targetEvents`,
		`delete (contents of targetEvent)`,
		`end repeat`,
		`if scopeText is not "future" then exit repeat`,
		`end if`,
		`end repeat`,
		`if foundAny is false then error "event not found"`,
		`return "ok"`,
		`end tell`,
		`end run`,
	}...), uid, string(resolvedScope), occUnix)
	if err == nil {
		return decodeWriteRejection(out)
	}
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

func nativeReminderMinutes(offset *time.Duration) (string, error) {
	if offset == nil {
		return "__ACAL_KEEP__", nil
	}
	if *offset%time.Minute != 0 {
		return "", fmt.Errorf("reminder offset must be a whole number of minutes")
	}
	return strconv.FormatInt(int64(*offset/time.Minute), 10), nil
}
