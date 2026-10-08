package app

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestSlotsRespectsEndWithDifferentOffsets(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"ACAL_CONFIG", "ACAL_PROFILE", "ACAL_TIMEZONE", "ACAL_FAIL_ON_DEGRADED"} {
		t.Setenv(name, "")
	}
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &fakeBackend{}, nil }
	t.Cleanup(func() { backendFactory = original })

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"slots", "--from", "2026-10-26T09:00:00+02:00", "--to", "2026-10-26T10:00:00+01:00", "--between", "09:00-17:00", "--duration", "1h", "--step", "1h", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("slots: %v: %s", err, &out)
	}
	var response struct{ Data []slotRow }
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("expected two slots within 07:00Z–09:00Z, got %d: %s", len(response.Data), &out)
	}
	for i, want := range []string{"2026-10-26T08:00:00Z", "2026-10-26T09:00:00Z"} {
		if got := response.Data[i].End.UTC().Format(time.RFC3339); got != want {
			t.Errorf("slot %d ends at %s, want %s", i, got, want)
		}
	}
}
