package app

import (
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestAllDayMoveCopyPreserveCalendarDates(t *testing.T) {
	for _, command := range []string{"move", "copy"} {
		for _, tc := range []struct {
			name, from, end, to, wantEnd string
		}{
			{"into spring", "2026-03-28", "2026-03-29", "2026-03-29", "2026-03-30T00:00:00+02:00"},
			{"into fall", "2026-10-24", "2026-10-25", "2026-10-25", "2026-10-26T00:00:00+01:00"},
			{"out of spring", "2026-03-29", "2026-03-30", "2026-04-02", "2026-04-03T00:00:00+02:00"},
			{"out of fall", "2026-10-25", "2026-10-26", "2026-11-02", "2026-11-03T00:00:00+01:00"},
			{"multiple dates", "2026-10-24", "2026-10-27", "2026-11-02", "2026-11-05T00:00:00+01:00"},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				setupReminderHistory(t)
				be := newIdentityBackend()
				event := mutationCommand[contract.Event](t, be, "events", "add", "--calendar", "Work", "--title", "All day", "--start", tc.from, "--end", tc.end, "--all-day", "--tz", "Europe/Berlin")
				// Native timestamps can use UTC regardless of the selected CLI zone.
				stored := be.records[event.ID]
				stored.Start, stored.End = stored.Start.UTC(), stored.End.UTC()
				be.records[event.ID] = stored
				args := []string{"events", command, event.ID, "--to", tc.to, "--tz", "Europe/Berlin", "--dry-run"}
				var end time.Time
				if command == "copy" {
					end = mutationCommand[backend.EventCreateInput](t, be, args...).End
				} else {
					preview := mutationCommand[backend.EventUpdateInput](t, be, args...)
					if preview.End == nil {
						t.Fatal("move preview omitted its end")
					}
					end = *preview.End
				}
				if got := end.Format(time.RFC3339); got != tc.wantEnd {
					t.Fatalf("end=%s; want exclusive date boundary %s", got, tc.wantEnd)
				}
			})
		}
	}
}
