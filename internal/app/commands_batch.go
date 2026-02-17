package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

type batchLine struct {
	Op       string  `json:"op"`
	ID       string  `json:"id,omitempty"`
	Calendar string  `json:"calendar,omitempty"`
	Title    *string `json:"title,omitempty"`
	Start    *string `json:"start,omitempty"`
	End      *string `json:"end,omitempty"`
	Duration *string `json:"duration,omitempty"`
	Location *string `json:"location,omitempty"`
	Notes    *string `json:"notes,omitempty"`
	URL      *string `json:"url,omitempty"`
	AllDay   *bool   `json:"all_day,omitempty"`
	Scope    string  `json:"scope,omitempty"`
}

func newEventsBatchCmd(opts *globalOptions) *cobra.Command {
	var filePath string
	var dryRun bool
	var continueOnError bool
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Apply add/update/delete operations from JSONL",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "events.batch")
			if err != nil {
				return err
			}
			if strings.TrimSpace(filePath) == "" {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("--file is required"), "Pass --file <path> or --file -", 2)
			}
			raw, err := readTextInput(filePath)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Check file path or stdin", 2)
			}
			loc := resolveLocation(ro.TZ)
			lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
			results := make([]map[string]any, 0)
			errorsCount := 0
			for i, line := range lines {
				s := strings.TrimSpace(line)
				if s == "" {
					continue
				}
				var row batchLine
				if err := json.Unmarshal([]byte(s), &row); err != nil {
					errorsCount++
					results = append(results, map[string]any{"line": i + 1, "ok": false, "error": "invalid json"})
					if !continueOnError {
						break
					}
					continue
				}
				res, execErr := executeBatchLine(context.Background(), be, row, loc, dryRun)
				if execErr != nil {
					errorsCount++
					results = append(results, map[string]any{"line": i + 1, "op": row.Op, "ok": false, "error": execErr.Error()})
					if !continueOnError {
						break
					}
					continue
				}
				res["line"] = i + 1
				res["ok"] = true
				results = append(results, res)
			}
			meta := map[string]any{"count": len(results), "errors": errorsCount, "dry_run": dryRun}
			if errorsCount > 0 {
				_ = p.Success(results, meta, nil)
				return Wrap(1, fmt.Errorf("batch completed with %d error(s)", errorsCount))
			}
			return p.Success(results, meta, nil)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSONL file path or - for stdin")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without writing")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", true, "Continue processing after row errors")
	return cmd
}

func executeBatchLine(ctx context.Context, be backend.Backend, row batchLine, loc *time.Location, dryRun bool) (map[string]any, error) {
	switch strings.ToLower(strings.TrimSpace(row.Op)) {
	case "add":
		if strings.TrimSpace(row.Calendar) == "" || row.Title == nil || row.Start == nil {
			return nil, fmt.Errorf("add requires calendar, title, start")
		}
		start, err := timeparse.ParseDateTime(*row.Start, time.Now(), loc)
		if err != nil {
			return nil, fmt.Errorf("invalid add.start")
		}
		end, err := resolveBatchEnd(row, start, loc)
		if err != nil {
			return nil, err
		}
		in := backend.EventCreateInput{Calendar: row.Calendar, Title: *row.Title, Start: start, End: end}
		if row.Location != nil {
			in.Location = *row.Location
		}
		if row.Notes != nil {
			in.Notes = *row.Notes
		}
		if row.URL != nil {
			in.URL = *row.URL
		}
		if row.AllDay != nil {
			in.AllDay = *row.AllDay
		}
		if dryRun {
			return map[string]any{"op": "add", "input": in}, nil
		}
		ev, err := be.AddEvent(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": "add", "id": ev.ID}, nil
	case "update":
		if strings.TrimSpace(row.ID) == "" {
			return nil, fmt.Errorf("update requires id")
		}
		scope, err := parseRecurrenceScope(row.Scope)
		if err != nil {
			return nil, err
		}
		in := backend.EventUpdateInput{Scope: scope}
		if row.Title != nil {
			in.Title = row.Title
		}
		if row.Location != nil {
			in.Location = row.Location
		}
		if row.Notes != nil {
			in.Notes = row.Notes
		}
		if row.URL != nil {
			in.URL = row.URL
		}
		if row.AllDay != nil {
			in.AllDay = row.AllDay
		}
		if row.Start != nil {
			ts, parseErr := timeparse.ParseDateTime(*row.Start, time.Now(), loc)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid update.start")
			}
			in.Start = &ts
		}
		if row.End != nil || row.Duration != nil {
			base := time.Now()
			if in.Start != nil {
				base = *in.Start
			}
			end, endErr := resolveBatchEnd(row, base, loc)
			if endErr != nil {
				return nil, endErr
			}
			in.End = &end
		}
		if dryRun {
			return map[string]any{"op": "update", "id": row.ID, "input": in}, nil
		}
		_, err = be.UpdateEvent(ctx, row.ID, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": "update", "id": row.ID}, nil
	case "delete":
		if strings.TrimSpace(row.ID) == "" {
			return nil, fmt.Errorf("delete requires id")
		}
		scope, err := parseRecurrenceScope(row.Scope)
		if err != nil {
			return nil, err
		}
		if dryRun {
			return map[string]any{"op": "delete", "id": row.ID, "scope": scope}, nil
		}
		if err := be.DeleteEvent(ctx, row.ID, scope); err != nil {
			return nil, err
		}
		return map[string]any{"op": "delete", "id": row.ID}, nil
	default:
		return nil, fmt.Errorf("unsupported op: %s", row.Op)
	}
}

func resolveBatchEnd(row batchLine, start time.Time, loc *time.Location) (time.Time, error) {
	if row.End != nil {
		end, err := timeparse.ParseDateTime(*row.End, time.Now(), loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid end")
		}
		if !end.After(start) {
			return time.Time{}, fmt.Errorf("end must be after start")
		}
		return end, nil
	}
	if row.Duration != nil {
		d, err := time.ParseDuration(*row.Duration)
		if err != nil || d <= 0 {
			return time.Time{}, fmt.Errorf("invalid duration")
		}
		return start.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("missing end or duration")
}
