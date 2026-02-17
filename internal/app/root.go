package app

import (
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

var backendFactory = selectBackend

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
		Version:       BuildVersionString(),
	}
	root.SetVersionTemplate("acal {{.Version}}\n")

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
	root.AddCommand(newVersionCmd())
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

	printer := output.Printer{
		Mode:          mode,
		Command:       command,
		Fields:        splitCSV(resolved.Fields),
		Quiet:         resolved.Quiet,
		SchemaVersion: resolved.SchemaVersion,
		Out:           cmd.OutOrStdout(),
		Err:           cmd.ErrOrStderr(),
	}

	be, err := backendFactory(resolved.Backend)
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

func parseRecurrenceScope(v string) (backend.RecurrenceScope, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "auto":
		return backend.ScopeAuto, nil
	case "this":
		return backend.ScopeThis, nil
	case "future":
		return backend.ScopeFuture, nil
	case "series":
		return backend.ScopeSeries, nil
	default:
		return backend.ScopeAuto, fmt.Errorf("invalid --scope: %s", v)
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
