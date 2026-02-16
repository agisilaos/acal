package app

import (
	"time"

	"github.com/agis/acal/internal/contract"
)

type daySummary struct {
	Date   string `json:"date"`
	Total  int    `json:"total"`
	AllDay int    `json:"all_day"`
	Timed  int    `json:"timed"`
}

func summarizeEventsByDay(events []contract.Event, from, to time.Time, loc *time.Location) []daySummary {
	if to.Before(from) {
		return nil
	}
	buckets := map[string]*daySummary{}
	for _, e := range events {
		day := e.Start.In(loc).Format("2006-01-02")
		row, ok := buckets[day]
		if !ok {
			row = &daySummary{Date: day}
			buckets[day] = row
		}
		row.Total++
		if e.AllDay {
			row.AllDay++
		} else {
			row.Timed++
		}
	}

	start, _ := dayBounds(from.In(loc))
	end, _ := dayBounds(to.In(loc))
	rows := make([]daySummary, 0, int(end.Sub(start)/(24*time.Hour))+1)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if row, ok := buckets[key]; ok {
			rows = append(rows, *row)
			continue
		}
		rows = append(rows, daySummary{Date: key})
	}
	return rows
}
