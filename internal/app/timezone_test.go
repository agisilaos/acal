package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestResolveLocation(t *testing.T) {
	for _, tz := range []string{"", "  ", "UTC", "Europe/Berlin", "Local"} {
		t.Run(tz, func(t *testing.T) {
			loc, err := resolveLocation(tz)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(tz) == "" {
				if loc != time.Local {
					t.Fatalf("got %v, want system local", loc)
				}
			} else if loc.String() != tz {
				t.Fatalf("got %v, want %s", loc, tz)
			}
		})
	}
	for _, tz := range []string{"Europe/Berln", "../UTC", " Europe/Berlin "} {
		if loc, err := resolveLocation(tz); err == nil || loc != nil || !strings.Contains(err.Error(), tz) {
			t.Fatalf("resolveLocation(%q) = %v, %v; want error naming value", tz, loc, err)
		}
	}
}

func TestInvalidEffectiveTimezone(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_PROFILE", "")
	t.Setenv("ACAL_TIMEZONE", "")
	t.Setenv("ACAL_OUTPUT", "")
	orig := backendFactory
	backendFactory = func(string) (backend.Backend, error) {
		t.Fatal("invalid timezone must fail before backend creation or health checks")
		return nil, nil
	}
	t.Cleanup(func() { backendFactory = orig })
	for _, source := range []string{"flag", "env", "config", "profile"} {
		t.Run(source, func(t *testing.T) {
			args := []string{"events", "add", "--calendar", "Work", "--title", "Review", "--start", "2026-10-01T09:00", "--duration", "30m", "--dry-run", "--json", "--fail-on-degraded"}
			switch source {
			case "flag":
				args = append(args, "--tz", "Europe/Berln")
			case "env":
				t.Setenv("ACAL_TIMEZONE", "Europe/Berln")
			default:
				content := "tz = 'Europe/Berln'\n"
				if source == "profile" {
					content = "tz = 'UTC'\n[profiles.default]\n" + content
				}
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			cmd := NewRootCommand()
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if ExitCode(err) != 2 {
				t.Fatalf("got %v (exit %d), want usage exit 2", err, ExitCode(err))
			}
			if out.Len() != 0 || !strings.Contains(stderr.String(), "Europe/Berln") || !strings.Contains(stderr.String(), `"code": "INVALID_USAGE"`) {
				t.Fatalf("unexpected output: stdout=%s stderr=%s", &out, &stderr)
			}
		})
	}
}

func TestTimezonePrecedenceAndLocalDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_PROFILE", "")
	t.Setenv("ACAL_TIMEZONE", "")
	orig := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = orig })
	for _, tc := range []struct {
		name, config, env string
		flags             []string
		want              string
	}{
		{name: "omitted", want: time.Local.String()},
		{name: "empty config", config: "", want: time.Local.String()},
		{name: "valid config", config: "Europe/Berlin", want: "Europe/Berlin"},
		{name: "env overrides invalid config", config: "Europe/Berln", env: "UTC", want: "UTC"},
		{name: "flag overrides invalid env", env: "Europe/Berln", flags: []string{"--tz", "Europe/Berlin"}, want: "Europe/Berlin"},
		{name: "empty flag overrides invalid env", env: "Europe/Berln", flags: []string{"--tz", ""}, want: time.Local.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ACAL_TIMEZONE", tc.env)
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("tz = '"+tc.config+"'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(append([]string{"events", "add", "--calendar", "Work", "--title", "Review", "--start", "2026-10-01T09:00", "--duration", "30m", "--dry-run", "--json", "--config", path}, tc.flags...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			loc, err := time.LoadLocation(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			want := time.Date(2026, 10, 1, 9, 0, 0, 0, loc).Format(time.RFC3339)
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output missing start %s: %s", want, &out)
			}
		})
	}
}

func TestEventFilterRejectsInvalidTimezone(t *testing.T) {
	_, err := buildEventFilterWithTZ("2026-10-01", "2026-10-02", nil, 0, "Europe/Berln")
	if err == nil || !strings.Contains(err.Error(), "Europe/Berln") {
		t.Fatalf("want timezone error, got %v", err)
	}
}
