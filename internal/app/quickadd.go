package app

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

var clockRe = regexp.MustCompile(`^\d{1,2}:\d{2}$`)

func newQuickAddCmd(opts *globalOptions) *cobra.Command {
	return newQuickAddCommand(opts, "quick-add <text>", "Create an event from natural text", "quick-add")
}

func newEventsQuickAddCmd(opts *globalOptions) *cobra.Command {
	return newQuickAddCommand(opts, "quick-add <text>", "Create an event from natural text", "events.quick-add")
}

func newQuickAddCommand(opts *globalOptions, use, short, commandName string) *cobra.Command {
	var calendar string
	var duration string
	var dryRun bool
	var allDay bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(c, opts, commandName)
			if err != nil {
				return err
			}
			loc := resolveLocation(ro.TZ)
			defaultDuration := 60 * time.Minute
			if strings.TrimSpace(duration) != "" {
				parsed, err := time.ParseDuration(duration)
				if err != nil || parsed <= 0 {
					_ = p.Error(contract.ErrInvalidUsage, "invalid --duration", "Use a positive Go duration like 30m or 1h")
					return Wrap(2, fmt.Errorf("invalid --duration: %q", duration))
				}
				defaultDuration = parsed
			}
			in, err := parseQuickAddInput(args[0], time.Now(), loc, calendar, defaultDuration, allDay)
			if err != nil {
				_ = p.Error(contract.ErrInvalidUsage, err.Error(), `Example: acal quick-add "tomorrow 10:00 Standup @Work 30m"`)
				return Wrap(2, err)
			}
			if dryRun {
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
	cmd.Flags().StringVar(&calendar, "calendar", "", "Default calendar if @Calendar is missing")
	cmd.Flags().StringVar(&duration, "duration", "1h", "Default duration if missing in text")
	cmd.Flags().BoolVar(&allDay, "all-day", false, "Create an all-day event")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without writing")
	return cmd
}

func parseQuickAddInput(input string, now time.Time, loc *time.Location, defaultCalendar string, defaultDuration time.Duration, allDay bool) (backend.EventCreateInput, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return backend.EventCreateInput{}, fmt.Errorf("input is required")
	}
	tokens := strings.Fields(text)
	start, consumed, hasTime, err := parseQuickAddStart(tokens, now, loc)
	if err != nil {
		return backend.EventCreateInput{}, err
	}
	if consumed >= len(tokens) {
		return backend.EventCreateInput{}, fmt.Errorf("missing title")
	}
	duration := defaultDuration
	calendar := strings.TrimSpace(defaultCalendar)
	titleParts := make([]string, 0, len(tokens)-consumed)
	for _, tok := range tokens[consumed:] {
		if strings.HasPrefix(tok, "@") && len(tok) > 1 {
			if calendar == "" {
				calendar = strings.TrimSpace(tok[1:])
				continue
			}
		}
		if d, ok := parseQuickAddDuration(tok); ok {
			duration = d
			continue
		}
		titleParts = append(titleParts, tok)
	}
	title := strings.TrimSpace(strings.Join(titleParts, " "))
	if title == "" {
		return backend.EventCreateInput{}, fmt.Errorf("missing title")
	}
	if calendar == "" {
		return backend.EventCreateInput{}, fmt.Errorf("missing calendar; include @Calendar or --calendar")
	}
	if !allDay && !hasTime {
		return backend.EventCreateInput{}, fmt.Errorf("missing time; include HH:MM or use --all-day")
	}
	if duration <= 0 {
		return backend.EventCreateInput{}, fmt.Errorf("duration must be positive")
	}
	end := start.Add(duration)
	if allDay {
		y, m, d := start.Date()
		start = time.Date(y, m, d, 0, 0, 0, 0, loc)
		end = start.Add(24 * time.Hour)
	}
	return backend.EventCreateInput{
		Calendar: calendar,
		Title:    title,
		Start:    start,
		End:      end,
		AllDay:   allDay,
	}, nil
}

func parseQuickAddStart(tokens []string, now time.Time, loc *time.Location) (time.Time, int, bool, error) {
	if len(tokens) == 0 {
		return time.Time{}, 0, false, fmt.Errorf("missing date/time")
	}
	if len(tokens) >= 2 && isDayToken(tokens[0]) && clockRe.MatchString(tokens[1]) {
		day, err := timeparse.ParseDateTime(tokens[0], now, loc)
		if err != nil {
			return time.Time{}, 0, false, fmt.Errorf("invalid day: %w", err)
		}
		hour, minute, err := parseClock(tokens[1])
		if err != nil {
			return time.Time{}, 0, false, err
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
		return start, 2, true, nil
	}
	if len(tokens) >= 2 {
		joined := tokens[0] + " " + tokens[1]
		if ts, err := timeparse.ParseDateTime(joined, now, loc); err == nil {
			return ts, 2, true, nil
		}
	}
	ts, err := timeparse.ParseDateTime(tokens[0], now, loc)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("invalid date/time")
	}
	return ts, 1, strings.Contains(tokens[0], ":"), nil
}

func parseClock(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time: %s", s)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid time: %s", s)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid time: %s", s)
	}
	return hour, minute, nil
}

func parseQuickAddDuration(token string) (time.Duration, bool) {
	if token == "" {
		return 0, false
	}
	d, err := time.ParseDuration(token)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

func isDayToken(token string) bool {
	s := strings.ToLower(strings.TrimSpace(token))
	if s == "today" || s == "tomorrow" || s == "yesterday" {
		return true
	}
	if strings.HasSuffix(s, "d") && (strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-")) {
		return true
	}
	if _, err := time.Parse("2006-01-02", token); err == nil {
		return true
	}
	return false
}
