package app

import (
	"os"
	"path/filepath"
	"testing"

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

	defaults := &globalOptions{Profile: "default", Backend: "default-backend", SchemaVersion: "v1", JSON: true}
	cmd := newTestCmd()
	if err := cmd.ParseFlags([]string{"--backend", "flag-backend", "--json"}); err != nil {
		t.Fatal(err)
	}
	defaults.Backend = "flag-backend"
	defaults.JSON = true

	resolved, err := resolveGlobalOptions(cmd, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Backend != "flag-backend" {
		t.Fatalf("expected flag backend, got %q", resolved.Backend)
	}
	if !resolved.JSON || resolved.JSONL || resolved.Plain {
		t.Fatalf("expected JSON mode from flag override, got json=%v jsonl=%v plain=%v", resolved.JSON, resolved.JSONL, resolved.Plain)
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
	t.Setenv("ACAL_PROFILE", "work")

	cfg := "backend='base-backend'\n[profiles.work]\nbackend='work-backend'\n"
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
	if resolved.Backend != "work-backend" {
		t.Fatalf("expected profile backend, got %q", resolved.Backend)
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
	cmd.Flags().String("profile", "default", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("backend", "", "")
	cmd.Flags().String("tz", "", "")
	cmd.Flags().String("schema-version", "v1", "")
	return cmd
}
