package output

import (
	"bytes"
	"strings"
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

func TestPrinterWritesToInjectedWriters(t *testing.T) {
	var out bytes.Buffer
	var errb bytes.Buffer
	p := Printer{
		Mode:    ModePlain,
		Command: "today",
		Fields:  []string{"id", "title"},
		Out:     &out,
		Err:     &errb,
	}

	if err := p.Success(contract.Event{ID: "evt-1", Title: "Standup"}, nil, nil); err != nil {
		t.Fatalf("success failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "evt-1\tStandup") {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if errb.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errb.String())
	}

	if err := p.Error(contract.ErrInvalidUsage, "bad input", "use --help"); err != nil {
		t.Fatalf("error output failed: %v", err)
	}
	if got := errb.String(); !strings.Contains(got, "error: bad input") {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestPrinterErrorRespectsNoColorAndEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var errb bytes.Buffer
	p := Printer{Err: &errb}
	if err := p.Error(contract.ErrInvalidUsage, "bad input", ""); err != nil {
		t.Fatalf("error output failed: %v", err)
	}
	got := errb.String()
	if strings.Contains(got, "\x1b[31m") {
		t.Fatalf("did not expect ansi color codes in %q", got)
	}

	errb.Reset()
	p = Printer{Err: &errb, NoColor: true}
	if err := p.Error(contract.ErrInvalidUsage, "bad input", ""); err != nil {
		t.Fatalf("error output failed: %v", err)
	}
	got = errb.String()
	if strings.Contains(got, "\x1b[31m") {
		t.Fatalf("did not expect ansi color codes with --no-color in %q", got)
	}
}

func TestPrinterErrorWithMetaJSON(t *testing.T) {
	var errb bytes.Buffer
	p := Printer{Mode: ModeJSON, Err: &errb}
	meta := map[string]any{"phase": "backend.list_events", "kind": "timeout"}
	if err := p.ErrorWithMeta(contract.ErrBackendUnavailable, "timeout", "retry", meta); err != nil {
		t.Fatalf("error output failed: %v", err)
	}
	got := errb.String()
	if !strings.Contains(got, `"meta":`) || !strings.Contains(got, `"phase": "backend.list_events"`) {
		t.Fatalf("expected meta fields in json error, got: %q", got)
	}
}
