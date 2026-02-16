package app

import (
	"context"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

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
