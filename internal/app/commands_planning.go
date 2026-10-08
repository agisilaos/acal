package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agis/acal/internal/contract"
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
			f.Overlap = true
			items, err := listEventsWithTimeout(ctx, be, f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			blocks := buildBusyBlocks(clipEventsToRange(items, f.From, f.To), includeAllDay)
			minutes := int64(0)
			for i := range blocks {
				minutes += blocks[i].Minutes
				blocks[i].Start = blocks[i].Start.In(ro.Location)
				blocks[i].End = blocks[i].End.In(ro.Location)
			}
			return successWithMeta(ctx, p, ro, blocks, map[string]any{"count": len(blocks), "busy_minutes": minutes, "events_scanned": len(items), "include_all_day": includeAllDay}, nil)
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID/name (repeatable CSV; use IDs or CSV quotes for names containing commas)")
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
			f.Overlap = true
			items, err := listEventsWithTimeout(ctx, be, f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			blocks := buildBusyBlocks(clipEventsToRange(items, f.From, f.To), includeAllDay)
			slots := buildSlots(blocks, f.From, f.To, startHour, startMinute, endHour, endMinute, dur, step)
			for i := range slots {
				slots[i].Start = slots[i].Start.In(ro.Location)
				slots[i].End = slots[i].End.In(ro.Location)
			}
			return successWithMeta(ctx, p, ro, slots, map[string]any{"count": len(slots), "duration_minutes": int64(dur.Minutes()), "events_scanned": len(items)}, nil)
		},
	}
	cmd.Flags().StringSliceVar(&calendars, "calendar", nil, "Calendar ID/name (repeatable CSV; use IDs or CSV quotes for names containing commas)")
	cmd.Flags().StringVar(&fromS, "from", "today", "Range start")
	cmd.Flags().StringVar(&toS, "to", "+14d", "Range end")
	cmd.Flags().StringVar(&between, "between", "09:00-17:00", "Daily window as HH:MM-HH:MM")
	cmd.Flags().StringVar(&durationS, "duration", "30m", "Required slot duration")
	cmd.Flags().StringVar(&stepS, "step", "15m", "Candidate step")
	cmd.Flags().IntVar(&limit, "limit", 0, "Limit events scanned")
	cmd.Flags().BoolVar(&includeAllDay, "include-all-day", false, "Include all-day events as busy")
	return cmd
}

// clipEventsToRange copies event values so planning never changes source identities
// or the original slice used to report events_scanned.
func clipEventsToRange(items []contract.Event, from, to time.Time) []contract.Event {
	clipped := make([]contract.Event, 0, len(items))
	for _, item := range items {
		item.Start = maxTime(item.Start, from)
		item.End = minTime(item.End, to)
		if item.Start.Before(item.End) {
			clipped = append(clipped, item)
		}
	}
	return clipped
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
	toDay, _ := dayBounds(to.In(from.Location()))
	slots := make([]slotRow, 0)
	for day := fromDay; !day.After(toDay); day = day.AddDate(0, 0, 1) {
		windowStart := time.Date(day.Year(), day.Month(), day.Day(), startHour, startMinute, 0, 0, day.Location())
		windowEnd := time.Date(day.Year(), day.Month(), day.Day(), endHour, endMinute, 0, 0, day.Location())
		windowStart = maxTime(windowStart, from)
		windowEnd = minTime(windowEnd, to)
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
