package app

import (
	"errors"
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

	if cfg, ok := readConfigFile(userPath); ok {
		applyFileConfig(&resolved, cfg, profile)
	}
	if cfg, ok := readConfigFile(projectPath); ok {
		applyFileConfig(&resolved, cfg, profile)
	}
	if configPath != "" && configPath != userPath && configPath != projectPath {
		if cfg, ok := readConfigFile(configPath); ok {
			applyFileConfig(&resolved, cfg, profile)
		}
	}

	applyEnv(&resolved)
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

func applyFileConfig(dst *globalOptions, cfg fileConfig, profile string) {
	if p, ok := cfg.Profiles[profile]; ok {
		cfg = mergeFileConfig(cfg, p)
	}
	if cfg.Backend != "" {
		dst.Backend = cfg.Backend
	}
	if cfg.TZ != "" {
		dst.TZ = cfg.TZ
	}
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			dst.Timeout = d
		}
	}
	if cfg.FailOnDegraded != nil {
		dst.FailOnDegraded = *cfg.FailOnDegraded
	}
	if cfg.Fields != "" {
		dst.Fields = cfg.Fields
	}
	applyOutputMode(dst, cfg.Output)
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

func applyEnv(dst *globalOptions) {
	if v := env("ACAL_BACKEND"); v != "" {
		dst.Backend = v
	}
	if v := env("ACAL_TIMEZONE"); v != "" {
		dst.TZ = v
	}
	if v := env("ACAL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			dst.Timeout = d
		}
	}
	if v := env("ACAL_FAIL_ON_DEGRADED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			dst.FailOnDegraded = b
		}
	}
	if v := env("ACAL_FIELDS"); v != "" {
		dst.Fields = v
	}
	applyOutputMode(dst, env("ACAL_OUTPUT"))
	if v := env("ACAL_NO_INPUT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			dst.NoInput = b
		}
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		dst.NoColor = true
	}
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

func applyOutputMode(dst *globalOptions, value string) {
	mode := output.Mode(strings.ToLower(value))
	switch mode {
	case output.ModeJSON, output.ModeJSONL, output.ModePlain:
		dst.OutputMode = mode
	}
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

func readConfigFile(path string) (fileConfig, bool) {
	if strings.TrimSpace(path) == "" {
		return fileConfig{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, false
	}
	var cfg fileConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, false
	}
	return cfg, true
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
