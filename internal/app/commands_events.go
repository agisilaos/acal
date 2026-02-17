package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

func newEventsCmd(opts *globalOptions) *cobra.Command {
	events := &cobra.Command{Use: "events", Short: "Event resources"}

	type conflictRow struct {
		LeftID           string    `json:"left_id"`
		LeftTitle        string    `json:"left_title"`
		LeftCalendar     string    `json:"left_calendar"`
		RightID          string    `json:"right_id"`
		RightTitle       string    `json:"right_title"`
		RightCalendar    string    `json:"right_calendar"`
		OverlapStart     time.Time `json:"overlap_start"`
		OverlapEnd       time.Time `json:"overlap_end"`
		OverlapMinutes   int64     `json:"overlap_minutes"`
		SameCalendarOnly bool      `json:"same_calendar_only"`
	}

	buildConflictRows := func(items []contract.Event, includeAllDay bool) []conflictRow {
		if len(items) < 2 {
			return nil
		}
		eventsCopy := make([]contract.Event, 0, len(items))
		for _, it := range items {
			if !includeAllDay && it.AllDay {
				continue
			}
			eventsCopy = append(eventsCopy, it)
		}
		if len(eventsCopy) < 2 {
			return nil
		}
		sort.Slice(eventsCopy, func(i, j int) bool {
			if eventsCopy[i].Start.Equal(eventsCopy[j].Start) {
				if eventsCopy[i].End.Equal(eventsCopy[j].End) {
					return eventsCopy[i].ID < eventsCopy[j].ID
				}
				return eventsCopy[i].End.Before(eventsCopy[j].End)
			}
			return eventsCopy[i].Start.Before(eventsCopy[j].Start)
		})

		rows := make([]conflictRow, 0)
		for i := 0; i < len(eventsCopy); i++ {
			for j := i + 1; j < len(eventsCopy); j++ {
				if !eventsCopy[j].Start.Before(eventsCopy[i].End) {
					break
				}
				overlapStart := maxTime(eventsCopy[i].Start, eventsCopy[j].Start)
				overlapEnd := minTime(eventsCopy[i].End, eventsCopy[j].End)
				if !overlapStart.Before(overlapEnd) {
					continue
				}
				leftCal := firstNonEmpty(eventsCopy[i].CalendarName, eventsCopy[i].CalendarID)
				rightCal := firstNonEmpty(eventsCopy[j].CalendarName, eventsCopy[j].CalendarID)
				rows = append(rows, conflictRow{
					LeftID:           eventsCopy[i].ID,
					LeftTitle:        eventsCopy[i].Title,
					LeftCalendar:     leftCal,
					RightID:          eventsCopy[j].ID,
					RightTitle:       eventsCopy[j].Title,
					RightCalendar:    rightCal,
					OverlapStart:     overlapStart,
					OverlapEnd:       overlapEnd,
					OverlapMinutes:   int64(overlapEnd.Sub(overlapStart).Minutes()),
					SameCalendarOnly: leftCal == rightCal,
				})
			}
		}
		return rows
	}

	var listCalendars []string
	var listFrom, listTo string
	var listLimit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.list")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(listFrom, listTo, listCalendars, listLimit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --from and --to with RFC3339, YYYY-MM-DD, or relative values", 2)
			}
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			return p.Success(items, map[string]any{"count": len(items)}, nil)
		},
	}
	list.Flags().StringSliceVar(&listCalendars, "calendar", nil, "Calendar ID or name (repeatable)")
	list.Flags().StringVar(&listFrom, "from", "today", "Range start")
	list.Flags().StringVar(&listTo, "to", "+7d", "Range end")
	list.Flags().IntVar(&listLimit, "limit", 0, "Limit results")

	var searchCalendars []string
	var searchFrom, searchTo, searchField string
	var searchLimit int
	search := &cobra.Command{
		Use:   "search <query>",
		Short: "Search events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.search")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(searchFrom, searchTo, searchCalendars, searchLimit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from/--to values", 2)
			}
			f.Query = args[0]
			f.Field = searchField
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			return p.Success(items, map[string]any{"count": len(items)}, nil)
		},
	}
	search.Flags().StringSliceVar(&searchCalendars, "calendar", nil, "Calendar ID or name (repeatable)")
	search.Flags().StringVar(&searchFrom, "from", "today", "Range start")
	search.Flags().StringVar(&searchTo, "to", "+30d", "Range end")
	search.Flags().StringVar(&searchField, "field", "all", "Search field: title|location|notes|all")
	search.Flags().IntVar(&searchLimit, "limit", 0, "Limit results")

	show := &cobra.Command{
		Use:   "show <event-id>",
		Short: "Show one event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, _, err := buildContext(cmd, opts, "events.show")
			if err != nil {
				return err
			}
			item, err := be.GetEventByID(context.Background(), args[0])
			if err != nil {
				return failWithHint(p, contract.ErrNotFound, err, "Check ID with `acal events list --fields id,title,start`", 4)
			}
			return p.Success(item, map[string]any{"count": 1}, nil)
		},
	}

	var queryCalendars, wheres []string
	var queryFrom, queryTo, sortField, order string
	var queryLimit int
	query := &cobra.Command{
		Use:   "query",
		Short: "Agent-focused deterministic query",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.query")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(queryFrom, queryTo, queryCalendars, queryLimit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from/--to values", 2)
			}
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			preds, err := parsePredicates(wheres)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use clauses like title~\"walk\" or calendar==\"Work\"", 2)
			}
			items, err = applyPredicates(items, preds)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Check --where field/operator/value", 2)
			}
			sortEvents(items, sortField, order)
			if queryLimit > 0 && len(items) > queryLimit {
				items = items[:queryLimit]
			}
			return p.Success(items, map[string]any{"count": len(items)}, nil)
		},
	}
	query.Flags().StringSliceVar(&queryCalendars, "calendar", nil, "Calendar ID or name (repeatable)")
	query.Flags().StringVar(&queryFrom, "from", "today", "Range start")
	query.Flags().StringVar(&queryTo, "to", "+30d", "Range end")
	query.Flags().StringSliceVar(&wheres, "where", nil, "Predicate clause (repeatable)")
	query.Flags().StringVar(&sortField, "sort", "start", "Sort field: start|end|title|updated_at|calendar")
	query.Flags().StringVar(&order, "order", "asc", "Sort order: asc|desc")
	query.Flags().IntVar(&queryLimit, "limit", 0, "Limit results")

	var conflictsCalendars []string
	var conflictsFrom, conflictsTo string
	var conflictsLimit int
	var conflictsIncludeAllDay bool
	conflicts := &cobra.Command{
		Use:   "conflicts",
		Short: "Detect overlapping events in a time range",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.conflicts")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(conflictsFrom, conflictsTo, conflictsCalendars, conflictsLimit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from/--to values", 2)
			}
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			rows := buildConflictRows(items, conflictsIncludeAllDay)
			meta := map[string]any{
				"count":           len(rows),
				"events_scanned":  len(items),
				"include_all_day": conflictsIncludeAllDay,
			}
			return p.Success(rows, meta, nil)
		},
	}
	conflicts.Flags().StringSliceVar(&conflictsCalendars, "calendar", nil, "Calendar ID or name (repeatable)")
	conflicts.Flags().StringVar(&conflictsFrom, "from", "today", "Range start")
	conflicts.Flags().StringVar(&conflictsTo, "to", "+30d", "Range end")
	conflicts.Flags().IntVar(&conflictsLimit, "limit", 0, "Limit scanned events before conflict analysis")
	conflicts.Flags().BoolVar(&conflictsIncludeAllDay, "include-all-day", false, "Include all-day events in overlap detection")

	var addCalendar, addTitle, addStart, addEnd, addDuration, addLocation, addNotes, addNotesFile, addURL, addRepeat string
	var addAllDay, addDryRun bool
	add := &cobra.Command{
		Use:   "add",
		Short: "Create an event",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.add")
			if err != nil {
				return err
			}
			if addCalendar == "" || addTitle == "" || addStart == "" {
				err = errors.New("--calendar, --title, and --start are required")
				return failWithHint(p, contract.ErrInvalidUsage, err, "Provide required fields", 2)
			}
			loc := resolveLocation(ro.TZ)
			startT, err := timeparse.ParseDateTime(addStart, time.Now(), loc)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Invalid --start format", 2)
			}
			endT, err := resolveEnd(addEnd, addDuration, startT, loc)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --end or --duration", 2)
			}
			notes := addNotes
			if addNotesFile != "" {
				notes, err = readTextInput(addNotesFile)
				if err != nil {
					return failWithHint(p, contract.ErrInvalidUsage, err, "Unable to read notes file", 2)
				}
			}
			in := backend.EventCreateInput{Calendar: addCalendar, Title: addTitle, Start: startT, End: endT, Location: addLocation, Notes: notes, URL: addURL, AllDay: addAllDay}
			spec, err := parseRepeatSpec(addRepeat, startT)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --repeat daily*5 | weekly:mon,wed*6 | monthly*3 | yearly*2", 2)
			}
			if spec.Frequency != "" {
				in.RepeatRule = canonicalRepeatRule(spec)
			}
			if addDryRun {
				return p.Success(in, map[string]any{"dry_run": true, "count": 1, "repeat": addRepeat}, nil)
			}
			item, err := be.AddEvent(context.Background(), in)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check calendar name and permissions", 1)
			}
			if item != nil {
				_ = appendHistory(historyEntry{Type: "add", EventID: item.ID, Created: item})
			}
			return p.Success(item, map[string]any{"count": 1, "repeat": addRepeat}, nil)
		},
	}
	add.Flags().StringVar(&addCalendar, "calendar", "", "Calendar ID or name")
	add.Flags().StringVar(&addTitle, "title", "", "Event title")
	add.Flags().StringVar(&addStart, "start", "", "Start datetime")
	add.Flags().StringVar(&addEnd, "end", "", "End datetime")
	add.Flags().StringVar(&addDuration, "duration", "", "Duration (e.g. 30m)")
	add.Flags().StringVar(&addLocation, "location", "", "Location")
	add.Flags().StringVar(&addNotes, "notes", "", "Notes")
	add.Flags().StringVar(&addNotesFile, "notes-file", "", "Notes path or - for stdin")
	add.Flags().StringVar(&addURL, "url", "", "URL")
	add.Flags().StringVar(&addRepeat, "repeat", "", "Repeat rule: daily*5, weekly:mon,wed*6, monthly*3, yearly*2")
	add.Flags().BoolVar(&addAllDay, "all-day", false, "All-day event")
	add.Flags().BoolVarP(&addDryRun, "dry-run", "n", false, "Preview without writing")

	var upTitle, upStart, upEnd, upDuration, upLocation, upNotes, upNotesFile, upURL, upScope, upRepeat string
	var upAllDay bool
	var upAllDaySet, upDryRun bool
	var ifMatch int
	update := &cobra.Command{
		Use:   "update <event-id>",
		Short: "Update an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.update")
			if err != nil {
				return err
			}
			current, getErr := be.GetEventByID(context.Background(), args[0])
			if getErr == nil && ifMatch > 0 && current.Sequence != ifMatch {
				err = fmt.Errorf("sequence mismatch: current=%d expected=%d", current.Sequence, ifMatch)
				return failWithHint(p, contract.ErrConcurrency, err, "Re-fetch event and retry", 7)
			}
			if getErr != nil && ifMatch > 0 {
				return failWithHint(p, contract.ErrNotFound, getErr, "Unable to verify sequence for --if-match-seq", 4)
			}
			scope, err := parseRecurrenceScope(upScope)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --scope auto|this|future|series", 2)
			}
			loc := resolveLocation(ro.TZ)
			patch := backend.EventUpdateInput{Scope: scope}
			if cmd.Flags().Changed("title") {
				patch.Title = &upTitle
			}
			if cmd.Flags().Changed("location") {
				patch.Location = &upLocation
			}
			if cmd.Flags().Changed("notes") || cmd.Flags().Changed("notes-file") {
				notes := upNotes
				if upNotesFile != "" {
					notes, err = readTextInput(upNotesFile)
					if err != nil {
						return failWithHint(p, contract.ErrInvalidUsage, err, "Unable to read notes file", 2)
					}
				}
				patch.Notes = &notes
			}
			if cmd.Flags().Changed("repeat") {
				spec, specErr := parseRepeatSpec(upRepeat, time.Now())
				if specErr != nil {
					return failWithHint(p, contract.ErrInvalidUsage, specErr, "Use --repeat daily*5 | weekly:mon,wed*6 | monthly*3 | yearly*2", 2)
				}
				r := canonicalRepeatRule(spec)
				patch.RepeatRule = &r
			}
			if cmd.Flags().Changed("url") {
				patch.URL = &upURL
			}
			if cmd.Flags().Changed("all-day") {
				upAllDaySet = true
			}
			if upAllDaySet {
				patch.AllDay = &upAllDay
			}
			if cmd.Flags().Changed("start") {
				t, e := timeparse.ParseDateTime(upStart, time.Now(), loc)
				if e != nil {
					return failWithHint(p, contract.ErrInvalidUsage, e, "Invalid --start", 2)
				}
				patch.Start = &t
			}
			if cmd.Flags().Changed("end") || cmd.Flags().Changed("duration") {
				base := time.Now()
				if current != nil {
					base = current.Start
				}
				if patch.Start != nil {
					base = *patch.Start
				}
				t, e := resolveEnd(upEnd, upDuration, base, loc)
				if e != nil {
					return failWithHint(p, contract.ErrInvalidUsage, e, "Use --end or --duration", 2)
				}
				patch.End = &t
			}
			if upDryRun {
				return p.Success(patch, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.UpdateEvent(context.Background(), args[0], patch)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Update failed", 1)
			}
			if current != nil {
				_ = appendHistory(historyEntry{Type: "update", EventID: args[0], Prev: current, Next: item})
			}
			return p.Success(item, map[string]any{"count": 1}, nil)
		},
	}
	update.Flags().StringVar(&upTitle, "title", "", "Event title")
	update.Flags().StringVar(&upStart, "start", "", "Start datetime")
	update.Flags().StringVar(&upEnd, "end", "", "End datetime")
	update.Flags().StringVar(&upDuration, "duration", "", "Duration (e.g. 30m)")
	update.Flags().StringVar(&upLocation, "location", "", "Location")
	update.Flags().StringVar(&upNotes, "notes", "", "Notes")
	update.Flags().StringVar(&upNotesFile, "notes-file", "", "Notes path or - for stdin")
	update.Flags().StringVar(&upURL, "url", "", "URL")
	update.Flags().StringVar(&upRepeat, "repeat", "", "Repeat metadata rule")
	update.Flags().BoolVar(&upAllDay, "all-day", false, "All-day event")
	update.Flags().StringVar(&upScope, "scope", "auto", "Recurrence scope: auto|this|future|series")
	update.Flags().IntVar(&ifMatch, "if-match-seq", 0, "Require matching sequence number")
	update.Flags().BoolVarP(&upDryRun, "dry-run", "n", false, "Preview without writing")

	var mvTo, mvBy, mvEnd, mvDuration, mvScope string
	var mvIfMatch int
	var mvDryRun bool
	move := &cobra.Command{
		Use:   "move <event-id>",
		Short: "Move an event to a new time",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.move")
			if err != nil {
				return err
			}
			current, getErr := be.GetEventByID(context.Background(), args[0])
			if getErr != nil {
				return failWithHint(p, contract.ErrNotFound, getErr, "Check ID with `acal events list --fields id,title,start`", 4)
			}
			if mvIfMatch > 0 && current.Sequence != mvIfMatch {
				err = fmt.Errorf("sequence mismatch: current=%d expected=%d", current.Sequence, mvIfMatch)
				return failWithHint(p, contract.ErrConcurrency, err, "Re-fetch event and retry", 7)
			}
			scope, err := parseRecurrenceScope(mvScope)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --scope auto|this|future|series", 2)
			}
			if (mvTo == "" && mvBy == "") || (mvTo != "" && mvBy != "") {
				err = errors.New("use exactly one of --to or --by")
				return failWithHint(p, contract.ErrInvalidUsage, err, "Set --to <datetime> or --by <duration>", 2)
			}
			loc := resolveLocation(ro.TZ)
			start := current.Start
			if mvTo != "" {
				start, err = timeparse.ParseDateTime(mvTo, time.Now(), loc)
				if err != nil {
					return failWithHint(p, contract.ErrInvalidUsage, err, "Invalid --to datetime", 2)
				}
			} else {
				by, parseErr := time.ParseDuration(mvBy)
				if parseErr != nil || by == 0 {
					if parseErr == nil {
						parseErr = errors.New("--by must not be zero")
					}
					return failWithHint(p, contract.ErrInvalidUsage, parseErr, "Use a duration like +30m, -1h, 2h", 2)
				}
				start = current.Start.Add(by)
			}

			var end time.Time
			if mvEnd != "" || mvDuration != "" {
				end, err = resolveEnd(mvEnd, mvDuration, start, loc)
				if err != nil {
					return failWithHint(p, contract.ErrInvalidUsage, err, "Use --end or --duration", 2)
				}
			} else {
				d := current.End.Sub(current.Start)
				if d <= 0 {
					err = errors.New("cannot preserve duration from current event; end must be after start")
					return failWithHint(p, contract.ErrInvalidUsage, err, "Pass --duration or --end explicitly", 2)
				}
				end = start.Add(d)
			}

			patch := backend.EventUpdateInput{
				Start: &start,
				End:   &end,
				Scope: scope,
			}
			if mvDryRun {
				return p.Success(patch, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.UpdateEvent(context.Background(), args[0], patch)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Move failed", 1)
			}
			_ = appendHistory(historyEntry{Type: "update", EventID: args[0], Prev: current, Next: item})
			return p.Success(item, map[string]any{"count": 1}, nil)
		},
	}
	move.Flags().StringVar(&mvTo, "to", "", "New start datetime")
	move.Flags().StringVar(&mvBy, "by", "", "Offset duration (e.g. 30m, -1h)")
	move.Flags().StringVar(&mvEnd, "end", "", "New end datetime")
	move.Flags().StringVar(&mvDuration, "duration", "", "New duration from start (e.g. 45m)")
	move.Flags().StringVar(&mvScope, "scope", "auto", "Recurrence scope: auto|this|future|series")
	move.Flags().IntVar(&mvIfMatch, "if-match-seq", 0, "Require matching sequence number")
	move.Flags().BoolVarP(&mvDryRun, "dry-run", "n", false, "Preview without writing")

	var cpTo, cpDuration, cpCalendar, cpTitle string
	var cpDryRun bool
	copyCmd := &cobra.Command{
		Use:   "copy <event-id>",
		Short: "Copy an event to a new time",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.copy")
			if err != nil {
				return err
			}
			if cpTo == "" {
				err = errors.New("--to is required")
				return failWithHint(p, contract.ErrInvalidUsage, err, "Set --to <datetime> for the copied event start", 2)
			}
			current, err := be.GetEventByID(context.Background(), args[0])
			if err != nil {
				return failWithHint(p, contract.ErrNotFound, err, "Check ID with `acal events list --fields id,title,start`", 4)
			}
			loc := resolveLocation(ro.TZ)
			start, err := timeparse.ParseDateTime(cpTo, time.Now(), loc)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Invalid --to datetime", 2)
			}
			duration := current.End.Sub(current.Start)
			if cpDuration != "" {
				duration, err = time.ParseDuration(cpDuration)
				if err != nil || duration <= 0 {
					if err == nil {
						err = errors.New("--duration must be positive")
					}
					return failWithHint(p, contract.ErrInvalidUsage, err, "Use a positive duration like 30m or 1h", 2)
				}
			}
			if duration <= 0 {
				err = errors.New("source event has invalid duration")
				return failWithHint(p, contract.ErrInvalidUsage, err, "Pass --duration explicitly", 2)
			}
			calendar := cpCalendar
			if calendar == "" {
				calendar = current.CalendarName
				if calendar == "" {
					calendar = current.CalendarID
				}
			}
			if calendar == "" {
				err = errors.New("source event calendar is empty")
				return failWithHint(p, contract.ErrInvalidUsage, err, "Pass --calendar for the destination calendar", 2)
			}
			title := cpTitle
			if title == "" {
				title = current.Title
			}
			in := backend.EventCreateInput{
				Calendar: calendar,
				Title:    title,
				Start:    start,
				End:      start.Add(duration),
				Location: current.Location,
				Notes:    current.Notes,
				URL:      current.URL,
				AllDay:   current.AllDay,
			}
			if cpDryRun {
				return p.Success(in, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.AddEvent(context.Background(), in)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Copy failed", 1)
			}
			if item != nil {
				_ = appendHistory(historyEntry{Type: "add", EventID: item.ID, Created: item})
			}
			return p.Success(item, map[string]any{"count": 1}, nil)
		},
	}
	copyCmd.Flags().StringVar(&cpTo, "to", "", "New start datetime")
	copyCmd.Flags().StringVar(&cpDuration, "duration", "", "Duration for copied event (defaults to source duration)")
	copyCmd.Flags().StringVar(&cpCalendar, "calendar", "", "Destination calendar (defaults to source)")
	copyCmd.Flags().StringVar(&cpTitle, "title", "", "Override copied title")
	copyCmd.Flags().BoolVarP(&cpDryRun, "dry-run", "n", false, "Preview without writing")

	var delForce, delDryRun bool
	var delConfirm, delScope string
	var delIfMatch int
	deleteCmd := &cobra.Command{
		Use:   "delete <event-id>",
		Short: "Delete an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "events.delete")
			if err != nil {
				return err
			}
			item, getErr := be.GetEventByID(context.Background(), args[0])
			if getErr == nil && delIfMatch > 0 && item.Sequence != delIfMatch {
				err = fmt.Errorf("sequence mismatch: current=%d expected=%d", item.Sequence, delIfMatch)
				return failWithHint(p, contract.ErrConcurrency, err, "Re-fetch event and retry", 7)
			}
			if getErr != nil && delIfMatch > 0 {
				return failWithHint(p, contract.ErrNotFound, getErr, "Unable to verify sequence for --if-match-seq", 4)
			}
			if !delForce && delConfirm != args[0] {
				if ro.NoInput || !stdinInteractive() {
					err = errors.New("non-interactive delete requires --force or --confirm <event-id>")
					return failWithHint(p, contract.ErrInvalidUsage, err, "Add --confirm exactly matching the event ID", 2)
				}
				ok, promptErr := promptConfirmID(os.Stdin, cmd.ErrOrStderr(), args[0])
				if promptErr != nil {
					return failWithHint(p, contract.ErrInvalidUsage, promptErr, "Use --force or --confirm <event-id> in non-interactive mode", 2)
				}
				if !ok {
					err = errors.New("delete confirmation mismatch")
					return failWithHint(p, contract.ErrInvalidUsage, err, "Use --force, or retry and enter the exact event ID", 2)
				}
			}
			scope, err := parseRecurrenceScope(delScope)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --scope auto|this|future|series", 2)
			}
			if delDryRun {
				if getErr != nil {
					item = &contract.Event{ID: args[0]}
				}
				return p.Success(item, map[string]any{"dry_run": true, "scope": scope}, nil)
			}
			if err := be.DeleteEvent(context.Background(), args[0], scope); err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Delete failed", 1)
			}
			if item != nil {
				_ = appendHistory(historyEntry{Type: "delete", EventID: args[0], Deleted: item})
			}
			return p.Success(map[string]any{"deleted": true, "id": args[0], "scope": scope}, map[string]any{"count": 1}, nil)
		},
	}
	deleteCmd.Flags().BoolVarP(&delForce, "force", "f", false, "Force delete without confirmation")
	deleteCmd.Flags().StringVar(&delConfirm, "confirm", "", "Confirm exact event ID")
	deleteCmd.Flags().StringVar(&delScope, "scope", "auto", "Recurrence scope: auto|this|future|series")
	deleteCmd.Flags().IntVar(&delIfMatch, "if-match-seq", 0, "Require matching sequence number")
	deleteCmd.Flags().BoolVarP(&delDryRun, "dry-run", "n", false, "Preview without writing")

	var remindAt string
	var remindClear, remindDryRun bool
	var remindIfMatch int
	remind := &cobra.Command{
		Use:   "remind <event-id>",
		Short: "Set or clear reminder metadata for an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, _, err := buildContext(cmd, opts, "events.remind")
			if err != nil {
				return err
			}
			if (strings.TrimSpace(remindAt) == "") == !remindClear {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("use exactly one of --at or --clear"), "Set --at <duration> or --clear", 2)
			}
			item, err := be.GetEventByID(context.Background(), args[0])
			if err != nil {
				return failWithHint(p, contract.ErrNotFound, err, "Check ID with `acal events list --fields id,title,start`", 4)
			}
			if remindIfMatch > 0 && item.Sequence != remindIfMatch {
				err = fmt.Errorf("sequence mismatch: current=%d expected=%d", item.Sequence, remindIfMatch)
				return failWithHint(p, contract.ErrConcurrency, err, "Re-fetch event and retry", 7)
			}
			meta := map[string]any{"count": 1}
			patch := backend.EventUpdateInput{Scope: backend.ScopeAuto}
			if remindClear {
				patch.ClearReminder = true
				meta["cleared"] = true
			} else {
				offset, parseErr := normalizeReminderOffset(remindAt)
				if parseErr != nil {
					return failWithHint(p, contract.ErrInvalidUsage, parseErr, "Use duration like -15m, 10m, 1h", 2)
				}
				patch.ReminderOffset = &offset
				meta["offset"] = offset.String()
			}
			if remindDryRun {
				return p.Success(patch, meta, nil)
			}
			updated, err := be.UpdateEvent(context.Background(), args[0], patch)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Reminder update failed", 1)
			}
			observed, verifyErr := be.GetReminderOffset(context.Background(), args[0])
			if verifyErr != nil {
				return failWithHint(p, contract.ErrGeneric, verifyErr, "Reminder updated but verification failed; retry `acal events show <id>`", 1)
			}
			if remindClear {
				if observed != nil {
					return failWithHint(p, contract.ErrGeneric, errors.New("reminder clear verification failed"), "Reminder still present after clear operation", 1)
				}
				meta["verified"] = true
			}
			if patch.ReminderOffset != nil {
				if observed == nil || *observed != *patch.ReminderOffset {
					return failWithHint(p, contract.ErrGeneric, errors.New("reminder offset verification failed"), "Observed reminder does not match requested offset", 1)
				}
				meta["verified"] = true
			}
			_ = appendHistory(historyEntry{Type: "update", EventID: args[0], Prev: item, Next: updated})
			return p.Success(updated, meta, nil)
		},
	}
	remind.Flags().StringVar(&remindAt, "at", "", "Reminder offset (e.g. -15m, 1h)")
	remind.Flags().BoolVar(&remindClear, "clear", false, "Clear reminder metadata marker")
	remind.Flags().IntVar(&remindIfMatch, "if-match-seq", 0, "Require matching sequence number")
	remind.Flags().BoolVarP(&remindDryRun, "dry-run", "n", false, "Preview without writing")

	events.AddCommand(list, search, show, query, conflicts, newEventsExportCmd(opts), newEventsImportCmd(opts), newEventsBatchCmd(opts), add, update, copyCmd, move, deleteCmd, remind, newEventsQuickAddCmd(opts))
	return events
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func failWithHint(printer output.Printer, code contract.ErrorCode, err error, hint string, exitCode int) error {
	if err == nil {
		err = errors.New("unknown error")
	}
	_ = printer.Error(code, err.Error(), hint)
	return Wrap(exitCode, err)
}
