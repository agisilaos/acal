package backend

import (
	"context"
	"time"

	"github.com/agis/acal/internal/contract"
)

type EventFilter struct {
	Calendars []string
	From      time.Time
	To        time.Time
	Limit     int
	Query     string
	Field     string
}

type EventCreateInput struct {
	Calendar string
	Title    string
	Start    time.Time
	End      time.Time
	Location string
	Notes    string
	URL      string
	AllDay   bool
}

type EventUpdateInput struct {
	Title    *string
	Start    *time.Time
	End      *time.Time
	Location *string
	Notes    *string
	URL      *string
	AllDay   *bool
}

type Backend interface {
	Doctor(context.Context) ([]contract.DoctorCheck, error)
	ListCalendars(context.Context) ([]contract.Calendar, error)
	ListEvents(context.Context, EventFilter) ([]contract.Event, error)
	GetEventByID(context.Context, string) (*contract.Event, error)
	AddEvent(context.Context, EventCreateInput) (*contract.Event, error)
	UpdateEvent(context.Context, string, EventUpdateInput) (*contract.Event, error)
	DeleteEvent(context.Context, string) error
}
