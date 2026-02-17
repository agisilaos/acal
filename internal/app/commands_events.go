package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

func newEventsCmd(opts *globalOptions) *cobra.Command {
	events := &cobra.Command{Use: "events", Short: "Event resources"}

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

	var addCalendar, addTitle, addStart, addEnd, addDuration, addLocation, addNotes, addNotesFile, addURL string
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
			if addDryRun {
				return p.Success(in, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.AddEvent(context.Background(), in)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check calendar name and permissions", 1)
			}
			return p.Success(item, map[string]any{"count": 1}, nil)
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
	add.Flags().BoolVar(&addAllDay, "all-day", false, "All-day event")
	add.Flags().BoolVarP(&addDryRun, "dry-run", "n", false, "Preview without writing")

	var upTitle, upStart, upEnd, upDuration, upLocation, upNotes, upNotesFile, upURL, upScope string
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
	update.Flags().BoolVar(&upAllDay, "all-day", false, "All-day event")
	update.Flags().StringVar(&upScope, "scope", "auto", "Recurrence scope: auto|this|future|series")
	update.Flags().IntVar(&ifMatch, "if-match-seq", 0, "Require matching sequence number")
	update.Flags().BoolVarP(&upDryRun, "dry-run", "n", false, "Preview without writing")

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
			return p.Success(map[string]any{"deleted": true, "id": args[0], "scope": scope}, map[string]any{"count": 1}, nil)
		},
	}
	deleteCmd.Flags().BoolVarP(&delForce, "force", "f", false, "Force delete without confirmation")
	deleteCmd.Flags().StringVar(&delConfirm, "confirm", "", "Confirm exact event ID")
	deleteCmd.Flags().StringVar(&delScope, "scope", "auto", "Recurrence scope: auto|this|future|series")
	deleteCmd.Flags().IntVar(&delIfMatch, "if-match-seq", 0, "Require matching sequence number")
	deleteCmd.Flags().BoolVarP(&delDryRun, "dry-run", "n", false, "Preview without writing")

	events.AddCommand(list, search, show, query, add, update, deleteCmd, newEventsQuickAddCmd(opts))
	return events
}

func failWithHint(printer output.Printer, code contract.ErrorCode, err error, hint string, exitCode int) error {
	if err == nil {
		err = errors.New("unknown error")
	}
	_ = printer.Error(code, err.Error(), hint)
	return Wrap(exitCode, err)
}
