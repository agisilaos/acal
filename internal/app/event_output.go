package app

import (
	"time"

	"github.com/agis/acal/internal/contract"
)

// Format returned events in the selected timezone without modifying backend
// records or the snapshots kept for history replay.
func eventOutputInLocation(data any, loc *time.Location) any {
	if loc == nil {
		return data
	}
	convert := func(event contract.Event) contract.Event {
		for _, field := range []*time.Time{&event.Start, &event.End, &event.UpdatedAt} {
			if !field.IsZero() {
				*field = field.In(loc)
			}
		}
		return event
	}
	switch events := data.(type) {
	case contract.Event:
		return convert(events)
	case *contract.Event:
		if events != nil {
			event := convert(*events)
			return &event
		}
	case []contract.Event:
		if events != nil {
			converted := make([]contract.Event, len(events))
			for i, event := range events {
				converted[i] = convert(event)
			}
			return converted
		}
	}
	return data
}
