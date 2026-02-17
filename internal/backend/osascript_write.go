package backend

import (
	"context"
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

	allDay := boolToScript(in.AllDay)
	startUnix := strconv.FormatInt(in.Start.Unix(), 10)
	endUnix := strconv.FormatInt(in.End.Unix(), 10)
	repeatText := strings.ToLower(strings.TrimSpace(in.RepeatRule))
	reminderMins := "__ACAL_KEEP__"
	if in.ReminderOffset != nil {
		reminderMins = strconv.Itoa(int(in.ReminderOffset.Minutes()))
	}
	out, err := runAppleScript([]string{
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
		`set epoch to date "1/1/1970 00:00:00"`,
		`set startDate to (epoch + (startText as integer))`,
		`set endDate to (epoch + (endText as integer))`,
		`tell application "Calendar"`,
		`set targetCal to missing value`,
		`try`,
		`set targetCal to first calendar whose name is calName`,
		`on error`,
		`error "calendar not found"`,
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
		`delete every display alarm of newEvent`,
		`make new display alarm at end of display alarms of newEvent with properties {trigger interval:(reminderText as integer)}`,
		`end if`,
		`return uid of newEvent as text`,
		`end tell`,
		`end run`,
	}, in.Calendar, in.Title, startUnix, endUnix, in.Location, in.Notes, in.URL, allDay, repeatText, reminderMins)
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
	repeatText := keep
	if in.RepeatRule != nil {
		repeatText = strings.ToLower(strings.TrimSpace(*in.RepeatRule))
	}
	reminderMins := keep
	if in.ReminderOffset != nil {
		reminderMins = strconv.Itoa(int(in.ReminderOffset.Minutes()))
	}
	clearReminder := "false"
	if in.ClearReminder {
		clearReminder = "true"
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
		`set repeatText to item 11 of argv`,
		`set reminderText to item 12 of argv`,
		`set clearReminderText to item 13 of argv`,
		`set epoch to date "1/1/1970 00:00:00"`,
		`tell application "Calendar"`,
		`set updatedUID to uidText`,
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
		`set targetEvents to every event of c whose uid is uidText and ((start date of it - epoch) as integer) is greater than or equal to occUnix`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText and ((start date of it - epoch) as integer) is occUnix}`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`end if`,
		`if (count of targetEvents) > 0 then`,
		`set foundAny to true`,
		`repeat with targetEvent in targetEvents`,
		`set targetRef to contents of targetEvent`,
		`if titleText is not "__ACAL_KEEP__" then set summary of targetRef to titleText`,
		`if locText is not "__ACAL_KEEP__" then set location of targetRef to locText`,
		`if notesText is not "__ACAL_KEEP__" then set description of targetRef to notesText`,
		`if urlText is not "__ACAL_KEEP__" then set url of targetRef to urlText`,
		`if startText is not "__ACAL_KEEP__" then set start date of targetRef to (epoch + (startText as integer))`,
		`if endText is not "__ACAL_KEEP__" then set end date of targetRef to (epoch + (endText as integer))`,
		`if allDayText is not "__ACAL_KEEP__" then`,
		`if allDayText is "true" then`,
		`set allday event of targetRef to true`,
		`else`,
		`set allday event of targetRef to false`,
		`end if`,
		`end if`,
		`if repeatText is not "__ACAL_KEEP__" then`,
		`if repeatText is "" then`,
		`set recurrence of targetRef to no recurrence`,
		`else`,
		`if repeatText starts with "daily" then set recurrence of targetRef to daily`,
		`if repeatText starts with "weekly" then set recurrence of targetRef to weekly`,
		`if repeatText starts with "monthly" then set recurrence of targetRef to monthly`,
		`if repeatText starts with "yearly" then set recurrence of targetRef to yearly`,
		`end if`,
		`end if`,
		`if clearReminderText is "true" then delete every display alarm of targetRef`,
		`if reminderText is not "__ACAL_KEEP__" then`,
		`delete every display alarm of targetRef`,
		`make new display alarm at end of display alarms of targetRef with properties {trigger interval:(reminderText as integer)}`,
		`end if`,
		`end repeat`,
		`if scopeText is not "future" then exit repeat`,
		`end if`,
		`end repeat`,
		`if foundAny is false then error "event not found"`,
		`return updatedUID`,
		`end tell`,
		`end run`,
	}, uid, string(scope), occUnix, title, start, end, location, notes, url, allDay, repeatText, reminderMins, clearReminder)
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
		`set targetEvents to every event of c whose uid is uidText and ((start date of it - epoch) as integer) is greater than or equal to occUnix`,
		`on error`,
		`set targetEvents to {}`,
		`end try`,
		`else`,
		`try`,
		`set targetEvents to {first event of c whose uid is uidText and ((start date of it - epoch) as integer) is occUnix}`,
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
