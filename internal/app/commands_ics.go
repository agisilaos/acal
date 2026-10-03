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
		Long:  "Export occurrences in the selected range as separate VEVENT entries. Recurrence rules and exceptions are not reconstructed.",
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
			ics := buildICS(items, ro.Location)
			meta := map[string]any{"count": len(items)}
			if strings.TrimSpace(outPath) != "" {
				file, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
				if err == nil {
					err = writeICS(file, ics)
					closeErr := file.Close()
					if err == nil {
						err = closeErr
					}
				}
				if err != nil {
					return failWithHint(p, contract.ErrGeneric, err, "Check destination path permissions", 1)
				}
				return successWithMeta(ctx, p, ro, map[string]any{"path": outPath, "events": len(items)}, meta, nil)
			}
			if m := p.EffectiveSuccessMode(); m == output.ModeJSON || m == output.ModeJSONL {
				return successWithMeta(ctx, p, ro, map[string]any{"ics": ics, "events": len(items)}, meta, nil)
			}
			return writeICS(c.OutOrStdout(), ics)
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID/name (repeatable CSV; use IDs or CSV quotes for names containing commas)")
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
		Long:  "Import independent events from ICS. Entries containing RRULE, RDATE, EXDATE, or RECURRENCE-ID are skipped with warnings. Use --strict to reject warnings before any writes.",
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
			items, warnings := parseICS(raw, calendar, ro.Location)
			if len(items) == 0 {
				hint := "Validate ICS content and DTSTART/DTEND fields"
				if len(warnings) > 0 {
					hint += "; " + strings.Join(warnings, "; ")
				}
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("no importable VEVENT entries"), hint, 2)
			}
			if strict && len(warnings) > 0 {
				return failWithHint(p, contract.ErrInvalidUsage, errors.New("strict import rejected warnings"), "Fix ICS warnings or omit --strict", 2)
			}
			if dryRun {
				return successWithMeta(ctx, p, ro, items, map[string]any{"count": len(items), "dry_run": true, "warnings": len(warnings)}, warnings)
			}
			created := make([]contract.Event, 0, len(items))
			for i, in := range items {
				ev, addErr := addEventWithTimeout(ctx, be, in)
				if addErr != nil {
					return failImport(p, addErr, created, i+1)
				}
				if ev != nil {
					created = append(created, *ev)
					if historyErr := appendHistory(historyEntry{Type: "add", EventID: ev.ID, Created: ev}); historyErr != nil {
						return failImport(p, fmt.Errorf("event created but history recording failed: %w", historyErr), created, i+1)
					}
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

// Imports are nontransactional. Report confirmed progress even if the last
// attempted backend write has an uncertain outcome.
func failImport(p output.Printer, err error, created []contract.Event, item int) error {
	ids := make([]string, 0, len(created))
	for _, event := range created {
		ids = append(ids, event.ID)
	}
	meta := backendErrorMeta(err)
	code, exitCode := contract.ErrGeneric, 1
	if meta == nil {
		meta = map[string]any{}
	} else if meta["kind"] != "creation_outcome_unknown" {
		code, exitCode = contract.ErrBackendUnavailable, 6
	}
	meta["created_ids"], meta["count"], meta["failed_item"] = ids, len(ids), item
	hint := fmt.Sprintf("Import stopped at item %d; %d confirmed creations: %q. Inspect Calendar and history before retrying; the failed attempt may also have completed. Undo recorded creations individually or delete by ID; retrying the whole file can duplicate events", item, len(ids), ids)
	if creationHint := creationInspectionHint(err); creationHint != "" {
		hint += ". " + creationHint
	}
	_ = p.ErrorWithMeta(code, err.Error(), hint, meta)
	return WrapPrinted(exitCode, err)
}

func buildICS(items []contract.Event, loc *time.Location) string {
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
			b.WriteString("DTSTART;VALUE=DATE:" + e.Start.In(loc).Format("20060102") + "\r\n")
			b.WriteString("DTEND;VALUE=DATE:" + e.End.In(loc).Format("20060102") + "\r\n")
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
	// Unfold content lines before recognizing property names and parameters.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.NewReplacer("\n ", "", "\n\t", "").Replace(raw)
	lines := strings.Split(raw, "\n")
	inEvent := false
	kv := map[string]string{}
	items := make([]backend.EventCreateInput, 0)
	warnings := make([]string, 0)
	flush := func() {
		if !inEvent {
			return
		}
		for _, property := range []string{"RRULE", "RDATE", "EXDATE", "RECURRENCE-ID"} {
			if _, present := kv[property]; present {
				warnings = append(warnings, "skipped VEVENT with unsupported recurrence property "+property)
				return
			}
		}
		title := strings.TrimSpace(kv["SUMMARY"])
		if title == "" {
			title = "Untitled"
		}
		start, allDayStart, okStart := parseICSDate(kv["DTSTART"], loc)
		end, allDayEnd, okEnd := parseICSDate(kv["DTEND"], loc)
		if !okStart || !okEnd || allDayStart != allDayEnd || !end.After(start) {
			warnings = append(warnings, "skipped VEVENT with invalid or unsupported DTSTART/DTEND (check matching VALUE types and IANA TZID)")
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
			AllDay:   allDayStart,
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
		keyRaw := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		key := strings.ToUpper(keyRaw)
		if strings.Contains(keyRaw, ";") {
			key = strings.ToUpper(strings.SplitN(keyRaw, ";", 2)[0])
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
	params := map[string]string{}
	for _, param := range strings.Split(parts[0], ";")[1:] {
		name, value, ok := strings.Cut(param, "=")
		name = strings.ToUpper(name)
		if !ok || value == "" {
			return time.Time{}, false, false
		}
		if _, duplicate := params[name]; duplicate {
			return time.Time{}, false, false
		}
		// Preserve parameter value casing when resolving IANA zone names.
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) && len(value) >= 2 {
			value = value[1 : len(value)-1]
		}
		if value == "" {
			return time.Time{}, false, false
		}
		params[name] = value
	}
	val := strings.TrimSpace(parts[1])
	valueType := params["VALUE"]
	if valueType != "" && !strings.EqualFold(valueType, "DATE") && !strings.EqualFold(valueType, "DATE-TIME") {
		return time.Time{}, false, false
	}
	zone, hasZone := params["TZID"]
	allDay := strings.EqualFold(valueType, "DATE")
	if hasZone {
		// DATE and UTC values must not carry TZID. Local is Go's process
		// timezone sentinel, not an IANA identifier from the input file.
		if allDay || strings.HasSuffix(val, "Z") || zone == "" || zone == "Local" {
			return time.Time{}, false, false
		}
		var err error
		loc, err = time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, false, false
		}
	}
	if allDay {
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

// writeICS checks the destination itself, including --out terminal device paths.
func writeICS(out io.Writer, ics string) error {
	if output.WriterIsTerminal(out) {
		ics = terminalICS(ics)
	}
	_, err := io.WriteString(out, ics)
	return err
}

// terminalICS preserves generated record separators while escaping data controls.
// Regular file and pipe exports bypass this display-only transformation.
func terminalICS(ics string) string {
	lines := strings.Split(ics, "\r\n")
	for i, line := range lines {
		lines[i] = output.EscapePlainControls(line)
	}
	return strings.Join(lines, "\r\n")
}
