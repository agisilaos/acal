package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/agis/acal/internal/output"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

type fileConfig struct {
	Backend        string                `toml:"backend"`
	TZ             string                `toml:"tz"`
	Timeout        string                `toml:"timeout"`
	FailOnDegraded *bool                 `toml:"fail_on_degraded"`
	Output         string                `toml:"output"`
	Fields         string                `toml:"fields"`
	Profile        string                `toml:"profile"`
	Profiles       map[string]fileConfig `toml:"profiles"`
}

func resolveGlobalOptions(cmd *cobra.Command, defaults *globalOptions) (*globalOptions, error) {
	resolved := *defaults
	if resolved.OutputMode == "" {
		resolved.OutputMode = output.ModeAuto
	}

	profile := firstNonEmpty(env("ACAL_PROFILE"), defaults.Profile)
	if flagValueChanged(cmd, "profile") {
		profile = defaults.Profile
	}
	if profile == "" {
		profile = "default"
	}
	resolved.Profile = profile

	userPath := defaultUserConfigPath()
	projectPath := ".acal.toml"
	configPath := firstNonEmpty(env("ACAL_CONFIG"), userPath)
	if flagValueChanged(cmd, "config") {
		configPath = defaults.Config
	}

	explicitConfig := flagValueChanged(cmd, "config") || env("ACAL_CONFIG") != ""
	if explicitConfig && strings.TrimSpace(configPath) == "" {
		return nil, errors.New("--config requires a non-empty file path")
	}
	paths := []string{userPath, projectPath}
	if configPath != "" && configPath != userPath && configPath != projectPath {
		paths = append(paths, configPath)
	}
	var cfg fileConfig
	sources := map[string]string{}
	for _, path := range paths {
		layer, err := readConfigFile(path, explicitConfig && path == configPath)
		if err != nil {
			return nil, err
		}
		source := fmt.Sprintf("config %q", path)
		if layer.Timeout != "" {
			sources["timeout"] = source + ": timeout"
		}
		if layer.Output != "" {
			sources["output"] = source + ": output"
		}
		if overlay, ok := layer.Profiles[profile]; ok {
			if overlay.Timeout != "" {
				sources["timeout"] = source + ": profiles." + profile + ".timeout"
			}
			if overlay.Output != "" {
				sources["output"] = source + ": profiles." + profile + ".output"
			}
			layer = mergeFileConfig(layer, overlay)
		}
		cfg = mergeFileConfig(cfg, layer)
	}
	if value := env("ACAL_TIMEOUT"); value != "" {
		cfg.Timeout, sources["timeout"] = value, "ACAL_TIMEOUT"
	}
	if value := env("ACAL_OUTPUT"); value != "" {
		cfg.Output, sources["output"] = value, "ACAL_OUTPUT"
	}
	if cfg.Timeout != "" && !flagValueChanged(cmd, "timeout") {
		duration, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid duration %q (use e.g. 15s, 1m, or 0): %w", sources["timeout"], cfg.Timeout, err)
		}
		resolved.Timeout = duration
	}
	if cfg.Output != "" && !hasEnabledOutputFlag(cmd) {
		switch mode := output.Mode(strings.ToLower(cfg.Output)); mode {
		case output.ModeJSON, output.ModeJSONL, output.ModePlain:
			resolved.OutputMode = mode
		default:
			return nil, fmt.Errorf("%s: invalid output %q (use json, jsonl, or plain)", sources["output"], cfg.Output)
		}
	}
	applyFileConfig(&resolved, cfg)
	if err := applyEnv(cmd, &resolved); err != nil {
		return nil, err
	}
	applyFlags(cmd, &resolved, defaults)
	mode, err := resolveOutputMode(cmd, resolved.OutputMode)
	if err != nil {
		return nil, err
	}
	resolved.OutputMode = mode

	if resolved.Config == "" {
		resolved.Config = configPath
	}
	return &resolved, nil
}

func applyFileConfig(dst *globalOptions, cfg fileConfig) {
	if cfg.Backend != "" {
		dst.Backend = cfg.Backend
	}
	if cfg.TZ != "" {
		dst.TZ = cfg.TZ
	}
	if cfg.FailOnDegraded != nil {
		dst.FailOnDegraded = *cfg.FailOnDegraded
	}
	if cfg.Fields != "" {
		dst.Fields = cfg.Fields
	}
}

func mergeFileConfig(base, overlay fileConfig) fileConfig {
	if overlay.Backend != "" {
		base.Backend = overlay.Backend
	}
	if overlay.TZ != "" {
		base.TZ = overlay.TZ
	}
	if overlay.Timeout != "" {
		base.Timeout = overlay.Timeout
	}
	if overlay.FailOnDegraded != nil {
		base.FailOnDegraded = overlay.FailOnDegraded
	}
	if overlay.Output != "" {
		base.Output = overlay.Output
	}
	if overlay.Fields != "" {
		base.Fields = overlay.Fields
	}
	if overlay.Profile != "" {
		base.Profile = overlay.Profile
	}
	return base
}

func applyEnv(cmd *cobra.Command, dst *globalOptions) error {
	if v := env("ACAL_BACKEND"); v != "" {
		dst.Backend = v
	}
	if v := env("ACAL_TIMEZONE"); v != "" {
		dst.TZ = v
	}
	if v := env("ACAL_FIELDS"); v != "" {
		dst.Fields = v
	}
	for _, setting := range []struct {
		name, flag string
		dst        *bool
	}{
		{"ACAL_FAIL_ON_DEGRADED", "fail-on-degraded", &dst.FailOnDegraded},
		{"ACAL_NO_INPUT", "no-input", &dst.NoInput},
	} {
		if value := env(setting.name); value != "" && !flagValueChanged(cmd, setting.flag) {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: invalid boolean %q (use true or false)", setting.name, value)
			}
			*setting.dst = parsed
		}
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		dst.NoColor = true
	}
	return nil
}

func applyFlags(cmd *cobra.Command, dst, fromFlags *globalOptions) {
	copyIfChanged(cmd, "fields", func() { dst.Fields = fromFlags.Fields })
	copyIfChanged(cmd, "quiet", func() { dst.Quiet = fromFlags.Quiet })
	copyIfChanged(cmd, "verbose", func() { dst.Verbose = fromFlags.Verbose })
	copyIfChanged(cmd, "no-color", func() { dst.NoColor = fromFlags.NoColor })
	copyIfChanged(cmd, "no-input", func() { dst.NoInput = fromFlags.NoInput })
	copyIfChanged(cmd, "fail-on-degraded", func() { dst.FailOnDegraded = fromFlags.FailOnDegraded })
	copyIfChanged(cmd, "profile", func() { dst.Profile = fromFlags.Profile })
	copyIfChanged(cmd, "config", func() { dst.Config = fromFlags.Config })
	copyIfChanged(cmd, "backend", func() { dst.Backend = fromFlags.Backend })
	copyIfChanged(cmd, "tz", func() { dst.TZ = fromFlags.TZ })
	copyIfChanged(cmd, "timeout", func() { dst.Timeout = fromFlags.Timeout })
	copyIfChanged(cmd, "schema-version", func() { dst.SchemaVersion = fromFlags.SchemaVersion })
}

func hasEnabledOutputFlag(cmd *cobra.Command) bool {
	for _, name := range []string{"json", "jsonl", "plain"} {
		if flag := cmd.Flag(name); flag != nil && flag.Changed && flag.Value.String() == "true" {
			return true
		}
	}
	return false
}

func resolveOutputMode(cmd *cobra.Command, inherited output.Mode) (output.Mode, error) {
	selected := output.ModeAuto
	for _, mode := range []output.Mode{output.ModeJSON, output.ModeJSONL, output.ModePlain} {
		flag := cmd.Flag(string(mode))
		if flag == nil || !flag.Changed {
			continue
		}
		enabled, err := strconv.ParseBool(flag.Value.String())
		if err != nil {
			return output.ModeAuto, err
		}
		if enabled {
			if selected != output.ModeAuto {
				return output.ModeAuto, errors.New("--json, --jsonl, and --plain are mutually exclusive")
			}
			selected = mode
		} else if inherited == mode {
			// An explicit false flag clears only its matching inherited mode.
			inherited = output.ModeAuto
		}
	}
	if selected != output.ModeAuto {
		return selected, nil
	}
	return inherited, nil
}

func copyIfChanged(cmd *cobra.Command, name string, fn func()) {
	if flagValueChanged(cmd, name) {
		fn()
	}
}

func flagValueChanged(cmd *cobra.Command, name string) bool {
	if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
		return true
	}
	if f := cmd.InheritedFlags().Lookup(name); f != nil && f.Changed {
		return true
	}
	return false
}

func readConfigFile(path string, required bool) (fileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return fileConfig{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, nil
		}
		return fileConfig{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg fileConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

func defaultUserConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "acal", "config.toml")
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "acal", "config.toml")
}

func env(k string) string { return strings.TrimSpace(os.Getenv(k)) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
