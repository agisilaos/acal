package app

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestAgendaCalendarDayBoundaries(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	cases := []struct {
		name, day, tz, start, end string
	}{
		{"UTC", "2026-02-08", "UTC", "2026-02-08T00:00:00Z", "2026-02-08T23:59:59Z"},
		{"spring", "2026-03-29", "Europe/Berlin", "2026-03-29T00:00:00+01:00", "2026-03-29T23:59:59+02:00"},
		{"fall", "2026-10-25", "Europe/Berlin", "2026-10-25T00:00:00+02:00", "2026-10-25T23:59:59+01:00"},
		{"spring timestamp", "2026-03-28T12:00", "Europe/Berlin", "2026-03-28T12:00:00+01:00", "2026-03-29T11:59:59+02:00"},
		{"fall timestamp", "2026-10-24T12:00", "Europe/Berlin", "2026-10-24T12:00:00+02:00", "2026-10-25T11:59:59+01:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, err := time.Parse(time.RFC3339, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			end, err := time.Parse(time.RFC3339, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			fb := &fakeBackend{events: []contract.Event{
				{ID: "before", Start: start.Add(-time.Second)},
				{ID: "start", Start: start},
				{ID: "end", Start: end},
				{ID: "after", Start: end.Add(time.Second)},
			}}
			previousFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = previousFactory })

			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"agenda", "--day", tc.day, "--tz", tc.tz, "--json"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Data []contract.Event `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Data) != 2 || got.Data[0].ID != "start" || got.Data[1].ID != "end" {
				t.Fatalf("expected only start and end events, got %+v", got.Data)
			}
		})
	}
}
