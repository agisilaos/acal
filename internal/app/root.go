package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

type globalOptions struct {
	JSON          bool
	JSONL         bool
	Plain         bool
	Fields        string
	Quiet         bool
	Verbose       bool
	NoColor       bool
	NoInput       bool
	Profile       string
	Config        string
	Backend       string
	TZ            string
	SchemaVersion string
}

func Execute() int {
	cmd := NewRootCommand()
	err := cmd.Execute()
	return ExitCode(err)
}

func NewRootCommand() *cobra.Command {
	opts := &globalOptions{Profile: "default", Backend: "osascript", SchemaVersion: contract.SchemaVersion}

	root := &cobra.Command{
		Use:           "acal",
		Short:         "Query and manage Apple Calendar from terminal workflows and agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVar(&opts.JSON, "json", false, "Output structured JSON")
	root.PersistentFlags().BoolVar(&opts.JSONL, "jsonl", false, "Output newline-delimited JSON")
	root.PersistentFlags().BoolVar(&opts.Plain, "plain", false, "Output stable plain text")
	root.PersistentFlags().StringVar(&opts.Fields, "fields", "", "Projected fields, comma-separated")
	root.PersistentFlags().BoolVarP(&opts.Quiet, "quiet", "q", false, "Reduce success output")
	root.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Verbose diagnostics")
	root.PersistentFlags().BoolVar(&opts.NoColor, "no-color", false, "Disable color output")
	root.PersistentFlags().BoolVar(&opts.NoInput, "no-input", false, "Disable prompts")
	root.PersistentFlags().StringVar(&opts.Profile, "profile", "default", "Config profile")
	root.PersistentFlags().StringVar(&opts.Config, "config", "", "Config file path")
	root.PersistentFlags().StringVar(&opts.Backend, "backend", "osascript", "Backend: osascript|eventkit")
	root.PersistentFlags().StringVar(&opts.TZ, "tz", "", "IANA timezone for output")
	root.PersistentFlags().StringVar(&opts.SchemaVersion, "schema-version", contract.SchemaVersion, "Output schema version")

	root.AddCommand(newSetupCmd(opts))
	root.AddCommand(newDoctorCmd(opts))
	root.AddCommand(newCalendarsCmd(opts))
	root.AddCommand(newEventsCmd(opts))
	root.AddCommand(newAgendaCmd(opts))
	root.AddCommand(newTodayCmd(opts))
	root.AddCommand(newWeekCmd(opts))
	root.AddCommand(newMonthCmd(opts))
	root.AddCommand(newViewCmd(opts))
	root.AddCommand(newQuickAddCmd(opts))
	root.AddCommand(newCompletionCmd(root))

	return root
}

func newDoctorCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run preflight checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "doctor")
			if err != nil {
				return err
			}
			_ = ro
			checks, derr := be.Doctor(context.Background())
			_ = p.Success(checks, map[string]any{"count": len(checks)}, nil)
			if derr != nil {
				_ = p.Error(contract.ErrBackendUnavailable, derr.Error(), "Run with GUI session and grant Calendar automation permission")
				return Wrap(6, derr)
			}
			return nil
		},
	}
}

func newCalendarsCmd(opts *globalOptions) *cobra.Command {
	calendars := &cobra.Command{Use: "calendars", Short: "Calendar resources"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List calendars",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, _, err := buildContext(cmd, opts, "calendars.list")
			if err != nil {
				return err
			}
			items, err := be.ListCalendars(context.Background())
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			return p.Success(items, map[string]any{"count": len(items)}, nil)
		},
	}
	calendars.AddCommand(list)
	return calendars
}

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
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --from and --to with RFC3339, YYYY-MM-DD, or relative values")
				return Wrap(2, err)
			}
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
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
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use valid --from/--to values")
				return Wrap(2, err)
			}
			f.Query = args[0]
			f.Field = searchField
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
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
				_ = p.Error(contract.ErrNotFound, err.Error(), "Check ID with `acal events list --fields id,title,start`")
				return Wrap(4, err)
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
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use valid --from/--to values")
				return Wrap(2, err)
			}
			items, err := be.ListEvents(context.Background(), f)
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			preds, err := parsePredicates(wheres)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use clauses like title~\"walk\" or calendar==\"Work\"")
				return Wrap(2, err)
			}
			items, err = applyPredicates(items, preds)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Check --where field/operator/value")
				return Wrap(2, err)
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
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Provide required fields")
				return Wrap(2, err)
			}
			loc := resolveLocation(ro.TZ)
			startT, err := timeparse.ParseDateTime(addStart, time.Now(), loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Invalid --start format")
				return Wrap(2, err)
			}
			endT, err := resolveEnd(addEnd, addDuration, startT, loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --end or --duration")
				return Wrap(2, err)
			}
			notes := addNotes
			if addNotesFile != "" {
				notes, err = readTextInput(addNotesFile)
				if err != nil {
					_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Unable to read notes file")
					return Wrap(2, err)
				}
			}
			in := backend.EventCreateInput{Calendar: addCalendar, Title: addTitle, Start: startT, End: endT, Location: addLocation, Notes: notes, URL: addURL, AllDay: addAllDay}
			if addDryRun {
				return p.Success(in, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.AddEvent(context.Background(), in)
			if err != nil {
				_ = p.Error(contract.ErrGeneric, err.Error(), "Check calendar name and permissions")
				return Wrap(1, err)
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

	var upTitle, upStart, upEnd, upDuration, upLocation, upNotes, upNotesFile, upURL string
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
				_ = p.Error(contract.ErrConcurrency, err.Error(), "Re-fetch event and retry")
				return Wrap(7, err)
			}
			if getErr != nil && ifMatch > 0 {
				_ = p.Error(contract.ErrNotFound, getErr.Error(), "Unable to verify sequence for --if-match-seq")
				return Wrap(4, getErr)
			}
			loc := resolveLocation(ro.TZ)
			var patch backend.EventUpdateInput
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
						_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Unable to read notes file")
						return Wrap(2, err)
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
					_ = p.Error(contract.ErrInvalidUsage, e.Error(), "Invalid --start")
					return Wrap(2, e)
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
					_ = p.Error(contract.ErrInvalidUsage, e.Error(), "Use --end or --duration")
					return Wrap(2, e)
				}
				patch.End = &t
			}
			if upDryRun {
				return p.Success(patch, map[string]any{"dry_run": true}, nil)
			}
			item, err := be.UpdateEvent(context.Background(), args[0], patch)
			if err != nil {
				_ = p.Error(contract.ErrGeneric, err.Error(), "Update failed")
				return Wrap(1, err)
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
	update.Flags().IntVar(&ifMatch, "if-match-seq", 0, "Require matching sequence number")
	update.Flags().BoolVarP(&upDryRun, "dry-run", "n", false, "Preview without writing")

	var delForce, delDryRun bool
	var delConfirm string
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
				_ = p.Error(contract.ErrConcurrency, err.Error(), "Re-fetch event and retry")
				return Wrap(7, err)
			}
			if getErr != nil && delIfMatch > 0 {
				_ = p.Error(contract.ErrNotFound, getErr.Error(), "Unable to verify sequence for --if-match-seq")
				return Wrap(4, getErr)
			}
			if !delForce && delConfirm != args[0] {
				if ro.NoInput {
					err = errors.New("non-interactive delete requires --force or --confirm <event-id>")
					_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Add --confirm exactly matching the event ID")
					return Wrap(2, err)
				}
				err = errors.New("delete requires --force or --confirm <event-id>")
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Pass --force or --confirm <event-id>")
				return Wrap(2, err)
			}
			if delDryRun {
				if getErr != nil {
					item = &contract.Event{ID: args[0]}
				}
				return p.Success(item, map[string]any{"dry_run": true}, nil)
			}
			if err := be.DeleteEvent(context.Background(), args[0]); err != nil {
				_ = p.Error(contract.ErrGeneric, err.Error(), "Delete failed")
				return Wrap(1, err)
			}
			return p.Success(map[string]any{"deleted": true, "id": args[0]}, map[string]any{"count": 1}, nil)
		},
	}
	deleteCmd.Flags().BoolVarP(&delForce, "force", "f", false, "Force delete without confirmation")
	deleteCmd.Flags().StringVar(&delConfirm, "confirm", "", "Confirm exact event ID")
	deleteCmd.Flags().IntVar(&delIfMatch, "if-match-seq", 0, "Require matching sequence number")
	deleteCmd.Flags().BoolVarP(&delDryRun, "dry-run", "n", false, "Preview without writing")

	events.AddCommand(list, search, show, query, add, update, deleteCmd, newEventsQuickAddCmd(opts))
	return events
}

func newAgendaCmd(opts *globalOptions) *cobra.Command {
	var day string
	var calendars []string
	var limit int
	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "Human-friendly agenda for a day",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "agenda")
			if err != nil {
				return err
			}
			loc := resolveLocation(ro.TZ)
			start, err := timeparse.ParseDateTime(day, time.Now(), loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use day as today, tomorrow, +Nd, or YYYY-MM-DD")
				return Wrap(2, err)
			}
			end := start.Add(24*time.Hour - time.Second)
			items, err := be.ListEvents(context.Background(), backend.EventFilter{From: start, To: end, Calendars: calendars, Limit: limit})
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			return p.Success(items, map[string]any{"count": len(items), "day": start.Format("2006-01-02")}, nil)
		},
	}
	cmd.Flags().StringVar(&day, "day", "today", "Day selector")
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit results")
	return cmd
}

func newTodayCmd(opts *globalOptions) *cobra.Command {
	var day string
	var calendars []string
	var limit int
	var summary bool
	cmd := &cobra.Command{
		Use:   "today",
		Short: "List events for a day (defaults to today)",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "today")
			if err != nil {
				return err
			}
			loc := resolveLocation(ro.TZ)
			anchor, err := timeparse.ParseDateTime(day, time.Now(), loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --day as today, tomorrow, +Nd, or YYYY-MM-DD")
				return Wrap(2, err)
			}
			start, end := dayBounds(anchor)
			items, err := be.ListEvents(context.Background(), backend.EventFilter{From: start, To: end, Calendars: calendars, Limit: limit})
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			if summary {
				rows := summarizeEventsByDay(items, start, end, loc)
				return p.Success(rows, map[string]any{"count": len(rows), "view": "day", "day": start.Format("2006-01-02"), "summary": true}, nil)
			}
			return p.Success(items, map[string]any{"count": len(items), "view": "day", "day": start.Format("2006-01-02")}, nil)
		},
	}
	cmd.Flags().StringVar(&day, "day", "today", "Day selector")
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit results")
	cmd.Flags().BoolVar(&summary, "summary", false, "Group by day with counts")
	return cmd
}

func newWeekCmd(opts *globalOptions) *cobra.Command {
	var of string
	var weekStart string
	var calendars []string
	var limit int
	var summary bool
	cmd := &cobra.Command{
		Use:   "week",
		Short: "List events for a week",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "week")
			if err != nil {
				return err
			}
			loc := resolveLocation(ro.TZ)
			anchor, err := timeparse.ParseDateTime(of, time.Now(), loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --of as today, tomorrow, +Nd, or YYYY-MM-DD")
				return Wrap(2, err)
			}
			ws, err := parseWeekStart(weekStart)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --week-start monday|sunday")
				return Wrap(2, err)
			}
			start, end := weekBounds(anchor, ws)
			items, err := be.ListEvents(context.Background(), backend.EventFilter{From: start, To: end, Calendars: calendars, Limit: limit})
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			if summary {
				rows := summarizeEventsByDay(items, start, end, loc)
				return p.Success(rows, map[string]any{"count": len(rows), "view": "week", "from": start.Format("2006-01-02"), "to": end.Format("2006-01-02"), "week_start": ws.String(), "summary": true}, nil)
			}
			return p.Success(items, map[string]any{"count": len(items), "view": "week", "from": start.Format("2006-01-02"), "to": end.Format("2006-01-02"), "week_start": ws.String()}, nil)
		},
	}
	cmd.Flags().StringVar(&of, "of", "today", "Date selector within target week")
	cmd.Flags().StringVar(&weekStart, "week-start", "monday", "Week start day: monday|sunday")
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit results")
	cmd.Flags().BoolVar(&summary, "summary", false, "Group by day with counts")
	return cmd
}

func newMonthCmd(opts *globalOptions) *cobra.Command {
	var month string
	var calendars []string
	var limit int
	var summary bool
	cmd := &cobra.Command{
		Use:   "month",
		Short: "List events for a month",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "month")
			if err != nil {
				return err
			}
			loc := resolveLocation(ro.TZ)
			anchor, err := parseMonthOrDate(month, time.Now(), loc)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), "Use --month as YYYY-MM, YYYY-MM-DD, or relative day syntax")
				return Wrap(2, err)
			}
			start, end := monthBounds(anchor)
			items, err := be.ListEvents(context.Background(), backend.EventFilter{From: start, To: end, Calendars: calendars, Limit: limit})
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return Wrap(6, err)
			}
			if summary {
				rows := summarizeEventsByDay(items, start, end, loc)
				return p.Success(rows, map[string]any{"count": len(rows), "view": "month", "month": start.Format("2006-01"), "from": start.Format("2006-01-02"), "to": end.Format("2006-01-02"), "summary": true}, nil)
			}
			return p.Success(items, map[string]any{"count": len(items), "view": "month", "month": start.Format("2006-01"), "from": start.Format("2006-01-02"), "to": end.Format("2006-01-02")}, nil)
		},
	}
	cmd.Flags().StringVar(&month, "month", "today", "Month selector: YYYY-MM, YYYY-MM-DD, today, +Nd")
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit results")
	cmd.Flags().BoolVar(&summary, "summary", false, "Group by day with counts")
	return cmd
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := strings.ToLower(args[0])
			switch shell {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return Wrap(2, fmt.Errorf("unsupported shell: %s", shell))
			}
		},
	}
}

func buildContext(cmd *cobra.Command, opts *globalOptions, command string) (output.Printer, backend.Backend, *globalOptions, error) {
	resolved, err := resolveGlobalOptions(cmd, opts)
	if err != nil {
		return output.Printer{}, nil, nil, Wrap(2, err)
	}
	if conflictCount(resolved.JSON, resolved.JSONL, resolved.Plain) > 1 {
		return output.Printer{}, nil, nil, Wrap(2, errors.New("--json, --jsonl, and --plain are mutually exclusive"))
	}
	mode := output.ModeAuto
	if resolved.JSON {
		mode = output.ModeJSON
	} else if resolved.JSONL {
		mode = output.ModeJSONL
	} else if resolved.Plain {
		mode = output.ModePlain
	}

	printer := output.Printer{Mode: mode, Command: command, Fields: splitCSV(resolved.Fields), Quiet: resolved.Quiet, SchemaVersion: resolved.SchemaVersion}

	be, err := selectBackend(resolved.Backend)
	if err != nil {
		_ = printer.Error(contract.ErrInvalidUsage, err.Error(), "Use --backend osascript")
		return printer, nil, nil, Wrap(2, err)
	}
	return printer, be, resolved, nil
}

func selectBackend(name string) (backend.Backend, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "osascript":
		return backend.NewOsaScriptBackend(), nil
	case "eventkit":
		return nil, fmt.Errorf("eventkit backend not implemented yet")
	default:
		return nil, fmt.Errorf("unknown backend: %s", name)
	}
}

func buildEventFilter(fromS, toS string, calendars []string, limit int) (backend.EventFilter, error) {
	return buildEventFilterWithTZ(fromS, toS, calendars, limit, "")
}

func buildEventFilterWithTZ(fromS, toS string, calendars []string, limit int, tz string) (backend.EventFilter, error) {
	loc := resolveLocation(tz)
	from, err := timeparse.ParseDateTime(fromS, time.Now(), loc)
	if err != nil {
		return backend.EventFilter{}, fmt.Errorf("invalid --from: %w", err)
	}
	to, err := timeparse.ParseDateTime(toS, time.Now(), loc)
	if err != nil {
		return backend.EventFilter{}, fmt.Errorf("invalid --to: %w", err)
	}
	if to.Before(from) {
		return backend.EventFilter{}, fmt.Errorf("--to must not be earlier than --from")
	}
	if to.Hour() == 0 && to.Minute() == 0 && to.Second() == 0 {
		to = to.Add(24*time.Hour - time.Second)
	}
	return backend.EventFilter{From: from, To: to, Calendars: calendars, Limit: limit}, nil
}

func resolveLocation(tz string) *time.Location {
	if strings.TrimSpace(tz) != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

func dayBounds(anchor time.Time) (time.Time, time.Time) {
	y, m, d := anchor.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, anchor.Location())
	end := start.AddDate(0, 0, 1).Add(-time.Second)
	return start, end
}

func weekBounds(anchor time.Time, weekStart time.Weekday) (time.Time, time.Time) {
	anchorStart, _ := dayBounds(anchor)
	delta := (int(anchorStart.Weekday()) - int(weekStart) + 7) % 7
	start := anchorStart.AddDate(0, 0, -delta)
	end := start.AddDate(0, 0, 7).Add(-time.Second)
	return start, end
}

func monthBounds(anchor time.Time) (time.Time, time.Time) {
	y, m, _ := anchor.Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, anchor.Location())
	end := start.AddDate(0, 1, 0).Add(-time.Second)
	return start, end
}

func parseWeekStart(v string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "monday", "mon":
		return time.Monday, nil
	case "sunday", "sun":
		return time.Sunday, nil
	default:
		return time.Sunday, fmt.Errorf("invalid --week-start: %s", v)
	}
}

func parseMonthOrDate(v string, now time.Time, loc *time.Location) (time.Time, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		s = "today"
	}
	if ts, err := time.ParseInLocation("2006-01", s, loc); err == nil {
		return ts, nil
	}
	return timeparse.ParseDateTime(s, now, loc)
}

func resolveEnd(endS, durationS string, start time.Time, loc *time.Location) (time.Time, error) {
	if strings.TrimSpace(endS) != "" && strings.TrimSpace(durationS) != "" {
		return time.Time{}, fmt.Errorf("use either --end or --duration, not both")
	}
	if strings.TrimSpace(endS) != "" {
		end, err := timeparse.ParseDateTime(endS, time.Now(), loc)
		if err != nil {
			return time.Time{}, err
		}
		if !end.After(start) {
			return time.Time{}, fmt.Errorf("--end must be after --start")
		}
		return end, nil
	}
	if strings.TrimSpace(durationS) != "" {
		d, err := time.ParseDuration(durationS)
		if err != nil {
			return time.Time{}, err
		}
		if d <= 0 {
			return time.Time{}, fmt.Errorf("--duration must be positive")
		}
		return start.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("missing --end or --duration")
}

func readTextInput(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func conflictCount(vals ...bool) int {
	total := 0
	for _, v := range vals {
		if v {
			total++
		}
	}
	return total
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
