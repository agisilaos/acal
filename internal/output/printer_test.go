package output

import (
	"testing"
	"time"

	"github.com/agis/acal/internal/contract"
)

func TestSchemaVersionDefault(t *testing.T) {
	p := Printer{}
	if p.schemaVersion() != contract.SchemaVersion {
		t.Fatalf("expected default schema version %q", contract.SchemaVersion)
	}
}

func TestFlattenWithFields(t *testing.T) {
	e := contract.Event{
		ID:    "abc",
		Title: "Standup",
		Start: time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC),
	}
	got := flatten(e, []string{"id", "title"})
	if got != "abc\tStandup" {
		t.Fatalf("unexpected flatten result: %q", got)
	}
}
