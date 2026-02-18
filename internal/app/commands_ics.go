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
	"github.com/spf13/cobra"
)

func newEventsExportCmd(opts *globalOptions) *cobra.Command {
	var calendars []string
	var fromS, toS, outPath string
	var limit int
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export events to ICS",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "events.export")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(fromS, toS, calendars, limit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from/--to values", 2)
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			items, err := listEventsWithTimeout(ctx, be, f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			ics := buildICS(items)
			meta := map[string]any{"count": len(items)}
			if strings.TrimSpace(outPath) != "" {
				if err := os.WriteFile(outPath, []byte(ics), 0o644); err != nil {
					return failWithHint(p, contract.ErrGeneric, err, "Check destination path permissions", 1)
				}
				return successWithMeta(ctx, p, ro, map[string]any{"path": outPath, "events": len(items)}, meta, nil)
			}
			if m := p.EffectiveSuccessMode(); m == output.ModeJSON || m == output.ModeJSONL {
				return successWithMeta(ctx, p, ro, map[string]any{"ics": ics, "events": len(items)}, meta, nil)
			}
			_, _ = fmt.Fprint(c.OutOrStdout(), ics)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name (repeatable)")
	cmd.Flags().StringVar(&fromS, "from", "today", "Range start")
	cmd.Flags().StringVar(&toS, "to", "+30d", "Range end")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit events exported")
	cmd.Flags().StringVar(&outPath, "out", "", "Output file path (default stdout)")
	return cmd
}

func newEventsImportCmd(opts *globalOptions) *cobra.Command {
	var filePath, calendar string
	var dryRun bool
	var strict bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import events from ICS",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "events.import")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			if strings.TrimSpace(filePath) == "" {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("--file is required"), "Pass --file <path> or --file - for stdin", 2)
			}
			if strings.TrimSpace(calendar) == "" {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("--calendar is required"), "Pass --calendar target calendar", 2)
			}
			raw, err := readICSInput(filePath)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Check --file path or stdin data", 2)
			}
			items, warnings := parseICS(raw, calendar, resolveLocation(ro.TZ))
			if len(items) == 0 {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("no importable VEVENT entries"), "Validate ICS content and DTSTART/DTEND fields", 2)
			}
			if strict && len(warnings) > 0 {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("strict import rejected warnings"), "Fix ICS warnings or omit --strict", 2)
			}
			if dryRun {
				return successWithMeta(ctx, p, ro, items, map[string]any{"count": len(items), "dry_run": true, "warnings": len(warnings)}, warnings)
			}
			created := make([]contract.Event, 0, len(items))
			for _, in := range items {
				ev, addErr := addEventWithTimeout(ctx, be, in)
				if addErr != nil {
					return failWithHint(p, contract.ErrGeneric, addErr, "Import failed; retry with --dry-run for diagnostics", 1)
				}
				if ev != nil {
					created = append(created, *ev)
				}
			}
			return successWithMeta(ctx, p, ro, created, map[string]any{"count": len(created), "warnings": len(warnings)}, warnings)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "ICS file path or - for stdin")
	cmd.Flags().StringVar(&calendar, "calendar", "", "Target calendar for imported events")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview import without writing")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat parser warnings as errors")
	return cmd
}

func buildICS(items []contract.Event) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//acal//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	now := time.Now().UTC().Format("20060102T150405Z")
	for _, e := range items {
		uid := e.ID
		if uid == "" {
			uid = fmt.Sprintf("acal-%d", e.Start.Unix())
		}
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:" + escapeICSText(uid) + "\r\n")
		b.WriteString("DTSTAMP:" + now + "\r\n")
		if e.AllDay {
			b.WriteString("DTSTART;VALUE=DATE:" + e.Start.UTC().Format("20060102") + "\r\n")
			b.WriteString("DTEND;VALUE=DATE:" + e.End.UTC().Format("20060102") + "\r\n")
		} else {
			b.WriteString("DTSTART:" + e.Start.UTC().Format("20060102T150405Z") + "\r\n")
			b.WriteString("DTEND:" + e.End.UTC().Format("20060102T150405Z") + "\r\n")
		}
		if strings.TrimSpace(e.Title) != "" {
			b.WriteString("SUMMARY:" + escapeICSText(e.Title) + "\r\n")
		}
		if strings.TrimSpace(e.Location) != "" {
			b.WriteString("LOCATION:" + escapeICSText(e.Location) + "\r\n")
		}
		if strings.TrimSpace(e.Notes) != "" {
			b.WriteString("DESCRIPTION:" + escapeICSText(e.Notes) + "\r\n")
		}
		if strings.TrimSpace(e.URL) != "" {
			b.WriteString("URL:" + escapeICSText(e.URL) + "\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func escapeICSText(v string) string {
	replacer := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n", "\r", "")
	return replacer.Replace(v)
}

func readICSInput(path string) (string, error) {
	if strings.TrimSpace(path) == "-" {
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

func parseICS(raw, calendar string, loc *time.Location) ([]backend.EventCreateInput, []string) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	inEvent := false
	kv := map[string]string{}
	items := make([]backend.EventCreateInput, 0)
	warnings := make([]string, 0)
	flush := func() {
		if !inEvent {
			return
		}
		title := strings.TrimSpace(kv["SUMMARY"])
		if title == "" {
			title = "Untitled"
		}
		start, allDayStart, okStart := parseICSDate(kv["DTSTART"], loc)
		end, allDayEnd, okEnd := parseICSDate(kv["DTEND"], loc)
		if !okStart || !okEnd || !end.After(start) {
			warnings = append(warnings, "skipped VEVENT with invalid DTSTART/DTEND")
			return
		}
		items = append(items, backend.EventCreateInput{
			Calendar: calendar,
			Title:    title,
			Start:    start,
			End:      end,
			Location: strings.TrimSpace(kv["LOCATION"]),
			Notes:    strings.TrimSpace(kv["DESCRIPTION"]),
			URL:      strings.TrimSpace(kv["URL"]),
			AllDay:   allDayStart || allDayEnd,
		})
	}

	for _, line := range lines {
		s := strings.TrimSpace(line)
		switch s {
		case "BEGIN:VEVENT":
			inEvent = true
			kv = map[string]string{}
			continue
		case "END:VEVENT":
			flush()
			inEvent = false
			kv = map[string]string{}
			continue
		}
		if !inEvent || s == "" {
			continue
		}
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			continue
		}
		keyRaw := strings.ToUpper(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		key := keyRaw
		if strings.Contains(keyRaw, ";") {
			key = strings.SplitN(keyRaw, ";", 2)[0]
		}
		if key == "DTSTART" || key == "DTEND" {
			kv[key] = keyRaw + ":" + value
			continue
		}
		kv[key] = value
	}
	return items, warnings
}

func parseICSDate(raw string, loc *time.Location) (time.Time, bool, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false, false
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, false, false
	}
	key := strings.ToUpper(parts[0])
	val := strings.TrimSpace(parts[1])
	if strings.Contains(key, "VALUE=DATE") {
		t, err := time.ParseInLocation("20060102", val, loc)
		if err != nil {
			return time.Time{}, true, false
		}
		return t, true, true
	}
	if strings.HasSuffix(val, "Z") {
		t, err := time.Parse("20060102T150405Z", val)
		if err == nil {
			return t, false, true
		}
	}
	t, err := time.ParseInLocation("20060102T150405", val, loc)
	if err != nil {
		return time.Time{}, false, false
	}
	return t, false, true
}
