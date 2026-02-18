package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/timeparse"
	"github.com/spf13/cobra"
)

type busyBlock struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Minutes int64     `json:"minutes"`
}

type slotRow struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Minutes int64     `json:"minutes"`
}

func newFreebusyCmd(opts *globalOptions) *cobra.Command {
	var calendars []string
	var fromS, toS string
	var limit int
	var includeAllDay bool
	cmd := &cobra.Command{
		Use:   "freebusy",
		Short: "Show merged busy intervals for a range",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "freebusy")
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
			blocks := buildBusyBlocks(items, includeAllDay)
			minutes := int64(0)
			for _, b := range blocks {
				minutes += b.Minutes
			}
			return p.Success(blocks, map[string]any{"count": len(blocks), "busy_minutes": minutes, "events_scanned": len(items), "include_all_day": includeAllDay}, nil)
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name (repeatable)")
	cmd.Flags().StringVar(&fromS, "from", "today", "Range start")
	cmd.Flags().StringVar(&toS, "to", "+30d", "Range end")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit events scanned")
	cmd.Flags().BoolVar(&includeAllDay, "include-all-day", false, "Include all-day events in busy calculation")
	return cmd
}

func newSlotsCmd(opts *globalOptions) *cobra.Command {
	var calendars []string
	var fromS, toS, between string
	var durationS, stepS string
	var limit int
	var includeAllDay bool
	cmd := &cobra.Command{
		Use:   "slots",
		Short: "Find available slots in a range",
		RunE: func(c *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(c, opts, "slots")
			if err != nil {
				return err
			}
			f, err := buildEventFilterWithTZ(fromS, toS, calendars, limit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from/--to values", 2)
			}
			dur, err := time.ParseDuration(durationS)
			if err != nil || dur <= 0 {
				if err == nil {
					err = fmt.Errorf("--duration must be positive")
				}
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --duration like 30m or 1h", 2)
			}
			step, err := time.ParseDuration(stepS)
			if err != nil || step <= 0 {
				if err == nil {
					err = fmt.Errorf("--step must be positive")
				}
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --step like 15m or 30m", 2)
			}
			if step > dur {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("--step must not exceed --duration"), "Set --step <= --duration", 2)
			}
			startHour, startMinute, endHour, endMinute, err := parseBetweenRange(between)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use --between HH:MM-HH:MM", 2)
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			items, err := listEventsWithTimeout(ctx, be, f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			blocks := buildBusyBlocks(items, includeAllDay)
			loc := resolveLocation(ro.TZ)
			anchorStart, err := timeparse.ParseDateTime(fromS, time.Now(), loc)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --from", 2)
			}
			anchorEnd, err := timeparse.ParseDateTime(toS, time.Now(), loc)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Use valid --to", 2)
			}
			if anchorEnd.Before(anchorStart) {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("--to must not be earlier than --from"), "Adjust range", 2)
			}
			slots := buildSlots(blocks, anchorStart, anchorEnd, startHour, startMinute, endHour, endMinute, dur, step)
			return p.Success(slots, map[string]any{"count": len(slots), "duration_minutes": int64(dur.Minutes()), "events_scanned": len(items)}, nil)
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID or name (repeatable)")
	cmd.Flags().StringVar(&fromS, "from", "today", "Range start")
	cmd.Flags().StringVar(&toS, "to", "+14d", "Range end")
	cmd.Flags().StringVar(&between, "between", "09:00-17:00", "Daily window as HH:MM-HH:MM")
	cmd.Flags().StringVar(&durationS, "duration", "30m", "Required slot duration")
	cmd.Flags().StringVar(&stepS, "step", "15m", "Candidate step")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit events scanned")
	cmd.Flags().BoolVar(&includeAllDay, "include-all-day", false, "Include all-day events as busy")
	return cmd
}

func buildBusyBlocks(items []contract.Event, includeAllDay bool) []busyBlock {
	if len(items) == 0 {
		return nil
	}
	ranges := make([]contract.Event, 0, len(items))
	for _, it := range items {
		if !includeAllDay && it.AllDay {
			continue
		}
		if !it.Start.Before(it.End) {
			continue
		}
		ranges = append(ranges, it)
	}
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start.Equal(ranges[j].Start) {
			return ranges[i].End.Before(ranges[j].End)
		}
		return ranges[i].Start.Before(ranges[j].Start)
	})
	merged := make([]busyBlock, 0, len(ranges))
	curStart := ranges[0].Start
	curEnd := ranges[0].End
	for i := 1; i < len(ranges); i++ {
		if !ranges[i].Start.After(curEnd) {
			if ranges[i].End.After(curEnd) {
				curEnd = ranges[i].End
			}
			continue
		}
		merged = append(merged, busyBlock{Start: curStart, End: curEnd, Minutes: int64(curEnd.Sub(curStart).Minutes())})
		curStart = ranges[i].Start
		curEnd = ranges[i].End
	}
	merged = append(merged, busyBlock{Start: curStart, End: curEnd, Minutes: int64(curEnd.Sub(curStart).Minutes())})
	return merged
}

func parseBetweenRange(v string) (int, int, int, int, error) {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) != 2 {
		return 0, 0, 0, 0, fmt.Errorf("invalid --between: %s", v)
	}
	aH, aM, err := parseClock(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	bH, bM, err := parseClock(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if bH < aH || (bH == aH && bM <= aM) {
		return 0, 0, 0, 0, fmt.Errorf("--between end must be after start")
	}
	return aH, aM, bH, bM, nil
}

func buildSlots(blocks []busyBlock, from, to time.Time, startHour, startMinute, endHour, endMinute int, duration, step time.Duration) []slotRow {
	fromDay, _ := dayBounds(from)
	toDay, _ := dayBounds(to)
	slots := make([]slotRow, 0)
	for day := fromDay; !day.After(toDay); day = day.AddDate(0, 0, 1) {
		windowStart := time.Date(day.Year(), day.Month(), day.Day(), startHour, startMinute, 0, 0, day.Location())
		windowEnd := time.Date(day.Year(), day.Month(), day.Day(), endHour, endMinute, 0, 0, day.Location())
		if day.Equal(fromDay) && from.After(windowStart) {
			windowStart = from
		}
		if day.Equal(toDay) && to.Before(windowEnd) {
			windowEnd = to
		}
		if !windowStart.Before(windowEnd) {
			continue
		}
		for candidate := windowStart; !candidate.Add(duration).After(windowEnd); candidate = candidate.Add(step) {
			candidateEnd := candidate.Add(duration)
			if overlapsBusy(candidate, candidateEnd, blocks) {
				continue
			}
			slots = append(slots, slotRow{Start: candidate, End: candidateEnd, Minutes: int64(duration.Minutes())})
		}
	}
	return slots
}

func overlapsBusy(start, end time.Time, blocks []busyBlock) bool {
	for _, b := range blocks {
		if !b.Start.Before(end) {
			continue
		}
		if start.Before(b.End) && b.Start.Before(end) {
			return true
		}
	}
	return false
}
