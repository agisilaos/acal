package output

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestPrinterPlainEscapesControls(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"readable", `Café 東京 👩‍💻 "quoted" C:\notes`, `Café 東京 👩‍💻 "quoted" C:\notes`},
		{"ansi", "before\x1b[2Jafter", `before\u001b[2Jafter`},
		{"osc", "before\x1b]52;c;payload\aafter", `before\u001b]52;c;payload\u0007after`},
		{"lines", "before\r\n\tafter", `before\r\n\tafter`},
		{"c1", "before\u009b2J\u009d52;c;payload\u009cafter", `before\u009b2J\u009d52;c;payload\u009cafter`},
		{"invalid_utf8", "before\x9b\xffafter", `before\x9b\xffafter`},
	}
	for r := rune(0); r <= 0x9f; r++ {
		if r > 0x1f && r < 0x7f {
			continue
		}
		want := fmt.Sprintf(`\u%04x`, r)
		switch r {
		case '\t':
			want = `\t`
		case '\n':
			want = `\n`
		case '\r':
			want = `\r`
		}
		cases = append(cases, struct{ name, text, want string }{fmt.Sprintf("U+%04X", r), string(r), want})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := contract.Event{ID: "evt-1", Title: tc.text}
			for _, data := range []any{event, &event, []contract.Event{event, event}} {
				var out bytes.Buffer
				p := Printer{Mode: ModePlain, Fields: []string{"id", "title"}, Out: &out}
				if err := p.Success(data, nil, nil); err != nil {
					t.Fatal(err)
				}
				want := "evt-1\t" + tc.want + "\n"
				if _, ok := data.([]contract.Event); ok {
					want += want
				}
				if got := out.String(); got != want {
					t.Fatalf("%T: got %q, want %q", data, got, want)
				}
			}
		})
	}
}

func TestPrinterPlainJSONFallbackEscapesControls(t *testing.T) {
	const title = "Café\x1b\x7f\u009b\u009d\u009c"
	for _, tc := range []struct {
		name   string
		data   any
		fields []string
	}{
		{"struct", contract.Event{Title: title}, nil},
		{"map", map[string]string{"title": title}, nil},
		{"map_with_fields", map[string]string{"title": title}, []string{"title"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			p := Printer{Mode: ModePlain, Fields: tc.fields, Out: &out}
			if err := p.Success(tc.data, nil, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), `Café\u001b\u007f\u009b\u009d\u009c`) {
				t.Fatalf("unexpected plain JSON: %q", out.String())
			}
			var got contract.Event
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Title != title {
				t.Fatalf("plain JSON changed data: title=%q err=%v", got.Title, err)
			}
		})
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
