package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agis/acal/internal/output"
	"github.com/spf13/cobra"
)

func TestResolveGlobalOptionsPrecedence(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_BACKEND", "env-backend")
	t.Setenv("ACAL_OUTPUT", "jsonl")

	userCfg := filepath.Join(tmp, ".config", "acal", "config.toml")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte("backend='user-backend'\noutput='plain'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".acal.toml"), []byte("backend='project-backend'\nfields='id,title'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	defaults := &globalOptions{Profile: "default", Backend: "default-backend", SchemaVersion: "v1"}
	cmd := newTestCmd()
	if err := cmd.ParseFlags([]string{"--backend", "flag-backend", "--json"}); err != nil {
		t.Fatal(err)
	}
	defaults.Backend = "flag-backend"

	resolved, err := resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Backend != "flag-backend" {
		t.Fatalf("expected flag backend, got %q", resolved.Backend)
	}
	if resolved.OutputMode != output.ModeJSON {
		t.Fatalf("expected JSON mode from flag override, got %q", resolved.OutputMode)
	}
	if resolved.Fields != "id,title" {
		t.Fatalf("expected fields from project config, got %q", resolved.Fields)
	}
}

func TestResolveGlobalOptionsProfile(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_PROFILE", "work")
	t.Setenv("ACAL_OUTPUT", "")

	cfg := "backend='base-backend'\noutput='plain'\n[profiles.work]\nbackend='work-backend'\noutput='jsonl'\n"
	if err := os.WriteFile(filepath.Join(tmp, ".acal.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	defaults := &globalOptions{Profile: "default", Backend: "default-backend", SchemaVersion: "v1"}
	resolved, err := resolveGlobalOptions(newTestCmd(), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Profile != "work" {
		t.Fatalf("expected work profile, got %q", resolved.Profile)
	}
	if resolved.OutputMode != output.ModeJSONL {
		t.Fatalf("expected profile output mode, got %q", resolved.OutputMode)
	}
	if resolved.Backend != "work-backend" {
		t.Fatalf("expected profile backend, got %q", resolved.Backend)
	}
}

func TestResolveGlobalOptionsNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	defaults := &globalOptions{Profile: "default", Backend: "osascript", SchemaVersion: "v1"}
	resolved, err := resolveGlobalOptions(newTestCmd(), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.NoColor {
		t.Fatalf("expected no-color when NO_COLOR is set")
	}
}

func TestResolveGlobalOptionsTimeoutEnvAndFlag(t *testing.T) {
	t.Setenv("ACAL_TIMEOUT", "45s")
	defaults := &globalOptions{Profile: "default", Backend: "osascript", Timeout: 15 * time.Second, SchemaVersion: "v1"}
	cmd := newTestCmd()
	resolved, err := resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Timeout, 45*time.Second; got != want {
		t.Fatalf("timeout mismatch from env: got=%s want=%s", got, want)
	}

	if err := cmd.ParseFlags([]string{"--timeout", "2m"}); err != nil {
		t.Fatal(err)
	}
	defaults.Timeout = 2 * time.Minute
	resolved, err = resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Timeout, 2*time.Minute; got != want {
		t.Fatalf("timeout mismatch from flag: got=%s want=%s", got, want)
	}
}

func TestResolveGlobalOptionsFailOnDegraded(t *testing.T) {
	t.Setenv("ACAL_FAIL_ON_DEGRADED", "true")
	defaults := &globalOptions{Profile: "default", Backend: "osascript", SchemaVersion: "v1"}
	cmd := newTestCmd()
	resolved, err := resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.FailOnDegraded {
		t.Fatalf("expected fail_on_degraded from env")
	}

	if err := cmd.ParseFlags([]string{"--fail-on-degraded=false"}); err != nil {
		t.Fatal(err)
	}
	defaults.FailOnDegraded = false
	resolved, err = resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.FailOnDegraded {
		t.Fatalf("expected flag override to false")
	}
}

func TestResolveGlobalOptionsOutputFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_PROFILE", "")

	// Columns are inherited auto, JSON, JSONL, and plain, respectively.
	const (
		auto  = output.ModeAuto
		json  = output.ModeJSON
		jsonl = output.ModeJSONL
		plain = output.ModePlain
	)
	cases := []struct {
		name string
		args []string
		want [4]output.Mode
	}{
		{"no flags", nil, [4]output.Mode{auto, json, jsonl, plain}},
		{"json true", []string{"--json"}, [4]output.Mode{json, json, json, json}},
		{"jsonl true", []string{"--jsonl"}, [4]output.Mode{jsonl, jsonl, jsonl, jsonl}},
		{"plain true", []string{"--plain"}, [4]output.Mode{plain, plain, plain, plain}},
		{"json false", []string{"--json=false"}, [4]output.Mode{auto, auto, jsonl, plain}},
		{"jsonl false", []string{"--jsonl=false"}, [4]output.Mode{auto, json, auto, plain}},
		{"plain false", []string{"--plain=false"}, [4]output.Mode{auto, json, jsonl, auto}},
		{"all false", []string{"--json=false", "--jsonl=false", "--plain=false"}, [4]output.Mode{auto, auto, auto, auto}},
		{"json true others false", []string{"--json", "--jsonl=false", "--plain=false"}, [4]output.Mode{json, json, json, json}},
		{"jsonl true others false", []string{"--json=false", "--jsonl", "--plain=false"}, [4]output.Mode{jsonl, jsonl, jsonl, jsonl}},
		{"plain true others false", []string{"--json=false", "--jsonl=false", "--plain"}, [4]output.Mode{plain, plain, plain, plain}},
		{"last json flag false", []string{"--json", "--json=false"}, [4]output.Mode{auto, auto, jsonl, plain}},
	}
	for i, inherited := range []string{"", "json", "jsonl", "plain"} {
		t.Run("inherited="+inherited, func(t *testing.T) {
			t.Setenv("ACAL_OUTPUT", inherited)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					cmd := newTestCmd()
					if err := cmd.ParseFlags(tc.args); err != nil {
						t.Fatal(err)
					}
					resolved, err := resolveGlobalOptions(cmd, &globalOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if resolved.OutputMode != tc.want[i] {
						t.Fatalf("mode = %q, want %q", resolved.OutputMode, tc.want[i])
					}
				})
			}
		})
	}
}

func TestResolveGlobalOptionsOutputConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_PROFILE", "work")

	cases := []struct {
		name   string
		config string
		env    string
		want   output.Mode
	}{
		{"file json", "output='json'", "", output.ModeJSON},
		{"file jsonl", "output='JSONL'", "", output.ModeJSONL},
		{"file plain", "output='plain'", "", output.ModePlain},
		{"unknown file", "output='unknown'", "", output.ModePlain},
		{"auto file", "output='auto'", "", output.ModePlain},
		{"untrimmed file", "output=' json '", "", output.ModePlain},
		{"unknown env", "output='json'", "unknown", output.ModeJSON},
		{"auto env", "output='jsonl'", "auto", output.ModeJSONL},
		{"trimmed env", "output='plain'", " JSON ", output.ModeJSON},
		{"profile", "output='json'\n[profiles.work]\noutput='jsonl'", "", output.ModeJSONL},
		{"unknown profile", "output='json'\n[profiles.work]\noutput='unknown'", "", output.ModePlain},
		{"auto profile", "output='json'\n[profiles.work]\noutput='auto'", "", output.ModePlain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ACAL_OUTPUT", tc.env)
			if err := os.WriteFile(".acal.toml", []byte(tc.config), 0o644); err != nil {
				t.Fatal(err)
			}
			resolved, err := resolveGlobalOptions(newTestCmd(), &globalOptions{OutputMode: output.ModePlain})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.OutputMode != tc.want {
				t.Fatalf("mode = %q, want %q", resolved.OutputMode, tc.want)
			}
		})
	}
}

func TestResolveGlobalOptionsInheritedOutputFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ACAL_CONFIG", "")
	t.Setenv("ACAL_OUTPUT", "json")

	root := NewRootCommand()
	if err := root.ParseFlags([]string{"--json=false"}); err != nil {
		t.Fatal(err)
	}
	child, _, err := root.Find([]string{"events", "list"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveGlobalOptions(child, &globalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.OutputMode != output.ModeAuto {
		t.Fatalf("mode = %q, want auto", resolved.OutputMode)
	}
}

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().Bool("jsonl", false, "")
	cmd.Flags().Bool("plain", false, "")
	cmd.Flags().String("fields", "", "")
	cmd.Flags().Bool("quiet", false, "")
	cmd.Flags().Bool("verbose", false, "")
	cmd.Flags().Bool("no-color", false, "")
	cmd.Flags().Bool("no-input", false, "")
	cmd.Flags().Bool("fail-on-degraded", false, "")
	cmd.Flags().String("profile", "default", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("backend", "", "")
	cmd.Flags().String("tz", "", "")
	cmd.Flags().Duration("timeout", 15*time.Second, "")
	cmd.Flags().String("schema-version", "v1", "")
	return cmd
}
