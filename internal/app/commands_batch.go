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

type batchExecResult struct {
	View    map[string]any
	History *historyEntry
}

func newEventsBatchCmd(opts *globalOptions) *cobra.Command {
	var filePath string
	var dryRun bool
	var continueOnError bool
	var strict bool
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
			if strict {
				continueOnError = false
			}
			raw, err := readTextInput(filePath)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Check file path or stdin", 2)
			}
			loc := resolveLocation(ro.TZ)
			ctx, cancel := commandContext(ro)
			defer cancel()
			txID := batchTxID()
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
					results = append(results, map[string]any{"tx_id": txID, "op_id": batchOpID(i+1, "parse"), "line": i + 1, "ok": false, "error": "invalid json"})
					if !continueOnError {
						break
					}
					continue
				}
				opID := batchOpID(i+1, row.Op)
				execRes, execErr := executeBatchLine(ctx, be, row, loc, dryRun)
				if execErr != nil {
					errorsCount++
					results = append(results, map[string]any{"tx_id": txID, "op_id": opID, "line": i + 1, "op": row.Op, "ok": false, "error": execErr.Error()})
					if !continueOnError {
						break
					}
					continue
				}
				if !dryRun && execRes.History != nil {
					execRes.History.TxID = txID
					execRes.History.OpID = opID
					if histErr := appendHistory(*execRes.History); histErr != nil {
						errorsCount++
						results = append(results, map[string]any{"tx_id": txID, "op_id": opID, "line": i + 1, "op": row.Op, "ok": false, "error": "failed to append history"})
						if !continueOnError {
							break
						}
						continue
					}
				}
				res := execRes.View
				res["tx_id"] = txID
				res["op_id"] = batchOpID(i+1, row.Op)
				res["line"] = i + 1
				res["ok"] = true
				results = append(results, res)
			}
			meta := map[string]any{"count": len(results), "errors": errorsCount, "dry_run": dryRun, "tx_id": txID}
			if errorsCount > 0 {
				_ = p.Success(results, meta, nil)
				return WrapPrinted(1, fmt.Errorf("batch completed with %d error(s)", errorsCount))
			}
			return p.Success(results, meta, nil)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSONL file path or - for stdin")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without writing")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", true, "Continue processing after row errors")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail fast on first row error")
	return cmd
}

func executeBatchLine(ctx context.Context, be backend.Backend, row batchLine, loc *time.Location, dryRun bool) (batchExecResult, error) {
	switch strings.ToLower(strings.TrimSpace(row.Op)) {
	case "add":
		if strings.TrimSpace(row.Calendar) == "" || row.Title == nil || row.Start == nil {
			return batchExecResult{}, fmt.Errorf("add requires calendar, title, start")
		}
		start, err := timeparse.ParseDateTime(*row.Start, time.Now(), loc)
		if err != nil {
			return batchExecResult{}, fmt.Errorf("invalid add.start")
		}
		end, err := resolveBatchEnd(row, start, loc)
		if err != nil {
			return batchExecResult{}, err
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
			return batchExecResult{View: map[string]any{"op": "add", "input": in}}, nil
		}
		ev, err := addEventWithTimeout(ctx, be, in)
		if err != nil {
			return batchExecResult{}, err
		}
		res := batchExecResult{View: map[string]any{"op": "add", "id": ev.ID}}
		if ev != nil {
			res.History = &historyEntry{Type: "add", EventID: ev.ID, Created: ev}
		}
		return res, nil
	case "update":
		if strings.TrimSpace(row.ID) == "" {
			return batchExecResult{}, fmt.Errorf("update requires id")
		}
		scope, err := parseRecurrenceScope(row.Scope)
		if err != nil {
			return batchExecResult{}, err
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
				return batchExecResult{}, fmt.Errorf("invalid update.start")
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
				return batchExecResult{}, endErr
			}
			in.End = &end
		}
		if dryRun {
			return batchExecResult{View: map[string]any{"op": "update", "id": row.ID, "input": in}}, nil
		}
		prev, err := getEventByIDWithTimeout(ctx, be, row.ID)
		if err != nil {
			return batchExecResult{}, fmt.Errorf("unable to snapshot event before update: %w", err)
		}
		next, err := updateEventWithTimeout(ctx, be, row.ID, in)
		if err != nil {
			return batchExecResult{}, err
		}
		return batchExecResult{
			View:    map[string]any{"op": "update", "id": row.ID},
			History: &historyEntry{Type: "update", EventID: row.ID, Prev: prev, Next: next},
		}, nil
	case "delete":
		if strings.TrimSpace(row.ID) == "" {
			return batchExecResult{}, fmt.Errorf("delete requires id")
		}
		scope, err := parseRecurrenceScope(row.Scope)
		if err != nil {
			return batchExecResult{}, err
		}
		if dryRun {
			return batchExecResult{View: map[string]any{"op": "delete", "id": row.ID, "scope": scope}}, nil
		}
		ev, err := getEventByIDWithTimeout(ctx, be, row.ID)
		if err != nil {
			return batchExecResult{}, fmt.Errorf("unable to snapshot event before delete: %w", err)
		}
		if err := deleteEventWithTimeout(ctx, be, row.ID, scope); err != nil {
			return batchExecResult{}, err
		}
		return batchExecResult{
			View:    map[string]any{"op": "delete", "id": row.ID},
			History: &historyEntry{Type: "delete", EventID: row.ID, Deleted: ev},
		}, nil
	default:
		return batchExecResult{}, fmt.Errorf("unsupported op: %s", row.Op)
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

func batchOpID(line int, op string) string {
	return fmt.Sprintf("op-%04d-%s", line, strings.ToLower(strings.TrimSpace(op)))
}

func batchTxID() string {
	return fmt.Sprintf("tx-%d", time.Now().UTC().UnixNano())
}
