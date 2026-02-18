package contract

import "time"

const SchemaVersion = "v1"

type ErrorCode string

const (
	ErrGeneric            ErrorCode = "GENERIC_FAILURE"
	ErrInvalidUsage       ErrorCode = "INVALID_USAGE"
	ErrPermissionDenied   ErrorCode = "PERMISSION_DENIED"
	ErrNotFound           ErrorCode = "NOT_FOUND"
	ErrConflict           ErrorCode = "CONFLICT"
	ErrBackendUnavailable ErrorCode = "BACKEND_UNAVAILABLE"
	ErrConcurrency        ErrorCode = "CONCURRENCY_CONFLICT"
)

type ErrorEnvelope struct {
	SchemaVersion string         `json:"schema_version"`
	Error         ErrorBody      `json:"error"`
	Meta          map[string]any `json:"meta,omitempty"`
}

type ErrorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Hint    string    `json:"hint,omitempty"`
}

type SuccessEnvelope struct {
	SchemaVersion string         `json:"schema_version"`
	Command       string         `json:"command"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Data          any            `json:"data"`
	Meta          map[string]any `json:"meta"`
	Warnings      []string       `json:"warnings"`
}

type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Writable bool   `json:"writable"`
}

type Event struct {
	ID           string    `json:"id"`
	CalendarID   string    `json:"calendar_id"`
	CalendarName string    `json:"calendar_name"`
	Title        string    `json:"title"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	AllDay       bool      `json:"all_day"`
	Location     string    `json:"location"`
	Notes        string    `json:"notes"`
	URL          string    `json:"url"`
	Sequence     int       `json:"sequence"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
