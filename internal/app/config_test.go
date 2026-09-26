package app

import (
	"os"
	"path/filepath"
	"strings"
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
		name      string
		config    string
		env       string
		want      output.Mode
		wantError bool
	}{
		{"file json", "output='json'", "", output.ModeJSON, false},
		{"file jsonl", "output='JSONL'", "", output.ModeJSONL, false},
		{"file plain", "output='plain'", "", output.ModePlain, false},
		{"unknown file", "output='unknown'", "", output.ModePlain, true},
		{"auto file", "output='auto'", "", output.ModePlain, true},
		{"untrimmed file", "output=' json '", "", output.ModePlain, true},
		{"unknown env", "output='json'", "unknown", output.ModeJSON, true},
		{"auto env", "output='jsonl'", "auto", output.ModeJSONL, true},
		{"trimmed env", "output='plain'", " JSON ", output.ModeJSON, false},
		{"profile", "output='json'\n[profiles.work]\noutput='jsonl'", "", output.ModeJSONL, false},
		{"unknown profile", "output='json'\n[profiles.work]\noutput='unknown'", "", output.ModePlain, true},
		{"auto profile", "output='json'\n[profiles.work]\noutput='auto'", "", output.ModePlain, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ACAL_OUTPUT", tc.env)
			if err := os.WriteFile(".acal.toml", []byte(tc.config), 0o644); err != nil {
				t.Fatal(err)
			}
			resolved, err := resolveGlobalOptions(newTestCmd(), &globalOptions{OutputMode: output.ModePlain})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "invalid output") {
					t.Fatalf("expected output error, got %v", err)
				}
				return
			}
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

// Keep validation independent of the developer's config and calendar data.
func isolateConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "user"))
	for _, key := range []string{"ACAL_CONFIG", "ACAL_PROFILE", "ACAL_BACKEND", "ACAL_TIMEZONE", "ACAL_TIMEOUT", "ACAL_OUTPUT", "ACAL_FIELDS", "ACAL_FAIL_ON_DEGRADED", "ACAL_NO_INPUT"} {
		t.Setenv(key, "")
	}
	return tmp
}

func TestConfigFileErrors(t *testing.T) {
	for _, tc := range []struct {
		name, path, contents, source string
		explicit, viaEnv, directory  bool
	}{
		{name: "optional absent"},
		{name: "explicit missing", path: "missing.toml", explicit: true, source: "read config"},
		{name: "env missing", path: "missing.toml", explicit: true, viaEnv: true, source: "read config"},
		{name: "explicit default missing", path: ".acal.toml", explicit: true, source: "read config"},
		{name: "explicit user missing", path: "user/acal/config.toml", explicit: true, source: "read config"},
		{name: "malformed explicit", path: "broken.toml", contents: "tz = [broken", explicit: true, source: "parse config"},
		{name: "malformed project", path: ".acal.toml", contents: "tz = [broken", source: "parse config"},
		{name: "malformed user", path: "user/acal/config.toml", contents: "tz = [broken", source: "parse config"},
		{name: "wrong type", path: ".acal.toml", contents: "fail_on_degraded='yes'", source: "parse config"},
		{name: "unreadable project", path: ".acal.toml", directory: true, source: "read config"},
		{name: "unreadable explicit", path: "dir", directory: true, explicit: true, source: "read config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			if tc.directory {
				if err := os.MkdirAll(tc.path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.contents != "" {
				if err := os.MkdirAll(filepath.Dir(tc.path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tc.path, []byte(tc.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd, defaults := newTestCmd(), &globalOptions{}
			if tc.explicit {
				if tc.viaEnv {
					t.Setenv("ACAL_CONFIG", tc.path)
				} else {
					if err := cmd.ParseFlags([]string{"--config", tc.path}); err != nil {
						t.Fatal(err)
					}
					defaults.Config = tc.path
				}
			}
			_, err := resolveGlobalOptions(cmd, defaults)
			if tc.source == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.source) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("expected source-aware %s error for %s, got %v", tc.source, tc.path, err)
			}
		})
	}
}

func TestEffectiveConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name, config, envKey, envValue, source string
		flags                                  []string
	}{
		{name: "file duration", config: "timeout='soon'", source: ".acal.toml\": timeout"},
		{name: "profile duration", config: "[profiles.default]\ntimeout='soon'", source: "profiles.default.timeout"},
		{name: "profile output", config: "[profiles.default]\noutput='jsno'", source: "profiles.default.output"},
		{name: "env duration", envKey: "ACAL_TIMEOUT", envValue: "soon", source: "ACAL_TIMEOUT"},
		{name: "env output", envKey: "ACAL_OUTPUT", envValue: "jsno", source: "ACAL_OUTPUT"},
		{name: "env fail boolean", envKey: "ACAL_FAIL_ON_DEGRADED", envValue: "maybe", source: "ACAL_FAIL_ON_DEGRADED"},
		{name: "env input boolean", envKey: "ACAL_NO_INPUT", envValue: "maybe", source: "ACAL_NO_INPUT"},
		{name: "flag shadows env duration", envKey: "ACAL_TIMEOUT", envValue: "soon", flags: []string{"--timeout=1s"}},
		{name: "flag shadows file duration", config: "timeout='soon'", flags: []string{"--timeout=1s"}},
		{name: "env shadows file duration", config: "timeout='soon'", envKey: "ACAL_TIMEOUT", envValue: "1s"},
		{name: "env shadows file output", config: "output='jsno'", envKey: "ACAL_OUTPUT", envValue: "json"},
		{name: "flag shadows env output", envKey: "ACAL_OUTPUT", envValue: "jsno", flags: []string{"--json"}},
		{name: "false flag does not shadow output", envKey: "ACAL_OUTPUT", envValue: "jsno", flags: []string{"--json=false"}, source: "ACAL_OUTPUT"},
		{name: "flag shadows fail boolean", envKey: "ACAL_FAIL_ON_DEGRADED", envValue: "maybe", flags: []string{"--fail-on-degraded=false"}},
		{name: "flag shadows input boolean", envKey: "ACAL_NO_INPUT", envValue: "maybe", flags: []string{"--no-input=false"}},
		{name: "profile shadows invalid base", config: "timeout='soon'\noutput='jsno'\n[profiles.default]\ntimeout='1s'\noutput='json'"},
		{name: "unknown keys and inactive profile", config: "future_key='ignored'\n[profiles.unselected]\ntimeout='soon'\noutput='jsno'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			if err := os.WriteFile(".acal.toml", []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.envKey != "" {
				t.Setenv(tc.envKey, tc.envValue)
			}
			cmd := newTestCmd()
			if err := cmd.ParseFlags(tc.flags); err != nil {
				t.Fatal(err)
			}
			_, err := resolveGlobalOptions(cmd, &globalOptions{})
			if tc.source == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.source) {
				t.Fatalf("expected error from %q, got %v", tc.source, err)
			}
		})
	}
}

func TestExplicitConfigPrecedence(t *testing.T) {
	tmp := isolateConfig(t)
	if err := os.MkdirAll(filepath.Dir(defaultUserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		defaultUserConfigPath(): "timeout='bad-user'\noutput='bad-user'",
		".acal.toml":            "timeout='2s'\noutput='jsonl'",
		"explicit.toml":         "timeout='3s'\noutput='plain'",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ACAL_CONFIG", "missing.toml")
	defaults := &globalOptions{Config: filepath.Join(tmp, "explicit.toml")}
	cmd := newTestCmd()
	if err := cmd.ParseFlags([]string{"--config", defaults.Config}); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Timeout != 3*time.Second || resolved.OutputMode != output.ModePlain {
		t.Fatalf("explicit config not applied: %+v", resolved)
	}
	t.Setenv("ACAL_TIMEOUT", "4s")
	t.Setenv("ACAL_OUTPUT", "json")
	resolved, err = resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Timeout != 4*time.Second || resolved.OutputMode != output.ModeJSON {
		t.Fatalf("env not applied: %+v", resolved)
	}
}

func TestQueriesListRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name, config, envKey, envValue, source string
		args                                   []string
	}{
		{name: "missing", args: []string{"--config", "missing.toml", "--json"}, source: "missing.toml"},
		{name: "malformed", config: "tz = [broken", args: []string{"--config", ".acal.toml", "--json"}, source: ".acal.toml"},
		{name: "output", envKey: "ACAL_OUTPUT", envValue: "jsno", source: "ACAL_OUTPUT"},
		{name: "timeout", envKey: "ACAL_TIMEOUT", envValue: "soon", source: "ACAL_TIMEOUT"},
		{name: "boolean", envKey: "ACAL_NO_INPUT", envValue: "maybe", source: "ACAL_NO_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			if tc.config != "" {
				if err := os.WriteFile(".acal.toml", []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.envKey != "" {
				t.Setenv(tc.envKey, tc.envValue)
			}
			root := NewRootCommand()
			root.SetArgs(append([]string{"queries", "list"}, tc.args...))
			err := root.Execute()
			if ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.source) {
				t.Fatalf("expected usage error naming %s, got %v", tc.source, err)
			}
		})
	}
}
