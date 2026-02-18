package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

var backendFactory = selectBackend

type globalOptions struct {
	JSON           bool
	JSONL          bool
	Plain          bool
	Fields         string
	Quiet          bool
	Verbose        bool
	NoColor        bool
	NoInput        bool
	FailOnDegraded bool
	Profile        string
	Config         string
	Backend        string
	TZ             string
	Timeout        time.Duration
	SchemaVersion  string
}

func Execute() int {
	cmd := NewRootCommand()
	err := cmd.Execute()
	if err != nil {
		renderTopLevelError(cmd, err)
	}
	return ExitCode(err)
}

func NewRootCommand() *cobra.Command {
	opts := &globalOptions{
		Profile:       "default",
		Backend:       "osascript",
		Timeout:       15 * time.Second,
		SchemaVersion: contract.SchemaVersion,
	}

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
	root.PersistentFlags().BoolVar(&opts.FailOnDegraded, "fail-on-degraded", false, "Fail if backend health is degraded")
	root.PersistentFlags().StringVar(&opts.Profile, "profile", "default", "Config profile")
	root.PersistentFlags().StringVar(&opts.Config, "config", "", "Config file path")
	root.PersistentFlags().StringVar(&opts.Backend, "backend", "osascript", "Backend: osascript|eventkit")
	root.PersistentFlags().StringVar(&opts.TZ, "tz", "", "IANA timezone for output")
	root.PersistentFlags().DurationVar(&opts.Timeout, "timeout", 15*time.Second, "Backend call timeout (e.g. 10s, 1m, 0 to disable)")
	root.PersistentFlags().StringVar(&opts.SchemaVersion, "schema-version", contract.SchemaVersion, "Output schema version")

	root.AddCommand(newSetupCmd(opts))
	root.AddCommand(newStatusCmd(opts))
	root.AddCommand(newVersionCmd())
	root.AddCommand(newDoctorCmd(opts))
	root.AddCommand(newCalendarsCmd(opts))
	root.AddCommand(newEventsCmd(opts))
	root.AddCommand(newAgendaCmd(opts))
	root.AddCommand(newFreebusyCmd(opts))
	root.AddCommand(newSlotsCmd(opts))
	root.AddCommand(newTodayCmd(opts))
	root.AddCommand(newWeekCmd(opts))
	root.AddCommand(newMonthCmd(opts))
	root.AddCommand(newViewCmd(opts))
	root.AddCommand(newHistoryCmd(opts))
	root.AddCommand(newQueriesCmd(opts))
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
		NoColor:       resolved.NoColor,
		SchemaVersion: resolved.SchemaVersion,
		Out:           cmd.OutOrStdout(),
		Err:           cmd.ErrOrStderr(),
	}

	be, err := backendFactory(resolved.Backend)
	if err != nil {
		_ = printer.Error(contract.ErrInvalidUsage, err.Error(), "Use --backend osascript")
		return printer, nil, nil, WrapPrinted(2, err)
	}
	if resolved.FailOnDegraded && !isHealthCommand(command) {
		ctx, cancel := commandContext(resolved)
		defer cancel()
		checks, derr := doctorWithTimeout(ctx, be)
		setup := buildSetupResult(checks, derr, resolved.Backend)
		if setup.Degraded {
			reasons := deriveDegradedReasonCodes(checks, derr)
			err = fmt.Errorf("degraded environment: %s", strings.Join(reasons, ","))
			_ = printer.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal status` and address next steps, or disable --fail-on-degraded")
			return printer, nil, nil, WrapPrinted(6, err)
		}
	}
	if resolved.Verbose {
		_, _ = fmt.Fprintf(printer.Err, "acal: command=%s backend=%s mode=%s tz=%s profile=%s timeout=%s\n", command, resolved.Backend, mode, resolved.TZ, resolved.Profile, resolved.Timeout)
	}
	return printer, be, resolved, nil
}

func commandContext(ro *globalOptions) (context.Context, context.CancelFunc) {
	timing := &timingRecorder{calls: map[string]time.Duration{}}
	base := context.WithValue(context.Background(), timingContextKey{}, timing)
	if ro == nil || ro.Timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, ro.Timeout)
}

type timeoutResult[T any] struct {
	val T
	err error
}

type timingContextKey struct{}

type timingRecorder struct {
	mu    sync.Mutex
	calls map[string]time.Duration
}

func (r *timingRecorder) add(name string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[name] += d
}

func backendTimings(ctx context.Context) map[string]string {
	rec, _ := ctx.Value(timingContextKey{}).(*timingRecorder)
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) == 0 {
		return nil
	}
	keys := make([]string, 0, len(rec.calls))
	for k := range rec.calls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = rec.calls[k].String()
	}
	return out
}

func withTimeout[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	ch := make(chan timeoutResult[T], 1)
	go func() {
		v, err := fn()
		ch <- timeoutResult[T]{val: v, err: err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case res := <-ch:
		return res.val, res.err
	}
}

func doctorWithTimeout(ctx context.Context, be backend.Backend) ([]contract.DoctorCheck, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() ([]contract.DoctorCheck, error) {
		return be.Doctor(ctx)
	})
	err = annotateBackendError(ctx, "backend.doctor", err)
	recordTiming(ctx, "backend.doctor", time.Since(start))
	return v, err
}

func listCalendarsWithTimeout(ctx context.Context, be backend.Backend) ([]contract.Calendar, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() ([]contract.Calendar, error) {
		return be.ListCalendars(ctx)
	})
	err = annotateBackendError(ctx, "backend.list_calendars", err)
	recordTiming(ctx, "backend.list_calendars", time.Since(start))
	return v, err
}

func listEventsWithTimeout(ctx context.Context, be backend.Backend, f backend.EventFilter) ([]contract.Event, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() ([]contract.Event, error) {
		return be.ListEvents(ctx, f)
	})
	err = annotateBackendError(ctx, "backend.list_events", err)
	recordTiming(ctx, "backend.list_events", time.Since(start))
	return v, err
}

func getEventByIDWithTimeout(ctx context.Context, be backend.Backend, id string) (*contract.Event, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() (*contract.Event, error) {
		return be.GetEventByID(ctx, id)
	})
	err = annotateBackendError(ctx, "backend.get_event_by_id", err)
	recordTiming(ctx, "backend.get_event_by_id", time.Since(start))
	return v, err
}

func addEventWithTimeout(ctx context.Context, be backend.Backend, in backend.EventCreateInput) (*contract.Event, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() (*contract.Event, error) {
		return be.AddEvent(ctx, in)
	})
	err = annotateBackendError(ctx, "backend.add_event", err)
	recordTiming(ctx, "backend.add_event", time.Since(start))
	return v, err
}

func updateEventWithTimeout(ctx context.Context, be backend.Backend, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() (*contract.Event, error) {
		return be.UpdateEvent(ctx, id, in)
	})
	err = annotateBackendError(ctx, "backend.update_event", err)
	recordTiming(ctx, "backend.update_event", time.Since(start))
	return v, err
}

func deleteEventWithTimeout(ctx context.Context, be backend.Backend, id string, scope backend.RecurrenceScope) error {
	start := time.Now()
	_, err := withTimeout(ctx, func() (struct{}, error) {
		return struct{}{}, be.DeleteEvent(ctx, id, scope)
	})
	err = annotateBackendError(ctx, "backend.delete_event", err)
	recordTiming(ctx, "backend.delete_event", time.Since(start))
	return err
}

func reminderOffsetWithTimeout(ctx context.Context, be backend.Backend, id string) (*time.Duration, error) {
	start := time.Now()
	v, err := withTimeout(ctx, func() (*time.Duration, error) {
		return be.GetReminderOffset(ctx, id)
	})
	err = annotateBackendError(ctx, "backend.get_reminder_offset", err)
	recordTiming(ctx, "backend.get_reminder_offset", time.Since(start))
	return v, err
}

func recordTiming(ctx context.Context, name string, d time.Duration) {
	rec, _ := ctx.Value(timingContextKey{}).(*timingRecorder)
	if rec == nil {
		return
	}
	rec.add(name, d)
}

func successWithMeta(ctx context.Context, p output.Printer, ro *globalOptions, data any, meta map[string]any, warnings []string) error {
	if ro != nil && ro.Verbose {
		timings := backendTimings(ctx)
		if len(timings) > 0 {
			if meta == nil {
				meta = map[string]any{}
			}
			meta["timings"] = timings
			_, _ = fmt.Fprintf(p.Err, "acal: timings=%v\n", timings)
		}
	}
	return p.Success(data, meta, warnings)
}

func isHealthCommand(command string) bool {
	return strings.HasPrefix(command, "doctor") ||
		strings.HasPrefix(command, "status") ||
		strings.HasPrefix(command, "setup")
}

func renderTopLevelError(cmd *cobra.Command, err error) {
	var appErr AppError
	if errors.As(err, &appErr) && appErr.Printed {
		return
	}
	if wantsStructuredErrorOutput(os.Args[1:]) {
		printer := output.Printer{
			Mode:          output.ModeJSON,
			SchemaVersion: contract.SchemaVersion,
			Err:           cmd.ErrOrStderr(),
		}
		_ = printer.Error(errorCodeForExit(ExitCode(err)), err.Error(), "")
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", err.Error())
}

func wantsStructuredErrorOutput(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "--":
			return false
		case arg == "--json", arg == "--jsonl":
			return true
		case strings.HasPrefix(arg, "--json="), strings.HasPrefix(arg, "--jsonl="):
			return true
		}
	}
	return false
}

func errorCodeForExit(code int) contract.ErrorCode {
	switch code {
	case 2:
		return contract.ErrInvalidUsage
	case 4:
		return contract.ErrNotFound
	case 6:
		return contract.ErrBackendUnavailable
	case 7:
		return contract.ErrConcurrency
	default:
		return contract.ErrGeneric
	}
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

func stdinInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func promptConfirmID(in io.Reader, out io.Writer, expected string) (bool, error) {
	if _, err := fmt.Fprintf(out, "Type event ID to confirm delete: "); err != nil {
		return false, err
	}
	var entered string
	if _, err := fmt.Fscanln(in, &entered); err != nil {
		return false, err
	}
	return strings.TrimSpace(entered) == strings.TrimSpace(expected), nil
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
