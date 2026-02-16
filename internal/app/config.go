package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

type fileConfig struct {
	Backend  string                `toml:"backend"`
	TZ       string                `toml:"tz"`
	Output   string                `toml:"output"`
	Fields   string                `toml:"fields"`
	Profile  string                `toml:"profile"`
	Profiles map[string]fileConfig `toml:"profiles"`
}

func resolveGlobalOptions(cmd *cobra.Command, defaults *globalOptions) (*globalOptions, error) {
	resolved := *defaults

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
	if cfg.Fields != "" {
		dst.Fields = cfg.Fields
	}
	if cfg.Output != "" {
		switch strings.ToLower(cfg.Output) {
		case "json":
			dst.JSON, dst.JSONL, dst.Plain = true, false, false
		case "jsonl":
			dst.JSON, dst.JSONL, dst.Plain = false, true, false
		case "plain":
			dst.JSON, dst.JSONL, dst.Plain = false, false, true
		}
	}
}

func mergeFileConfig(base, overlay fileConfig) fileConfig {
	if overlay.Backend != "" {
		base.Backend = overlay.Backend
	}
	if overlay.TZ != "" {
		base.TZ = overlay.TZ
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
	if v := env("ACAL_FIELDS"); v != "" {
		dst.Fields = v
	}
	if v := env("ACAL_OUTPUT"); v != "" {
		switch strings.ToLower(v) {
		case "json":
			dst.JSON, dst.JSONL, dst.Plain = true, false, false
		case "jsonl":
			dst.JSON, dst.JSONL, dst.Plain = false, true, false
		case "plain":
			dst.JSON, dst.JSONL, dst.Plain = false, false, true
		}
	}
	if v := env("ACAL_NO_INPUT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			dst.NoInput = b
		}
	}
}

func applyFlags(cmd *cobra.Command, dst, fromFlags *globalOptions) {
	copyIfChanged(cmd, "json", func() { dst.JSON = fromFlags.JSON })
	copyIfChanged(cmd, "jsonl", func() { dst.JSONL = fromFlags.JSONL })
	copyIfChanged(cmd, "plain", func() { dst.Plain = fromFlags.Plain })
	copyIfChanged(cmd, "fields", func() { dst.Fields = fromFlags.Fields })
	copyIfChanged(cmd, "quiet", func() { dst.Quiet = fromFlags.Quiet })
	copyIfChanged(cmd, "verbose", func() { dst.Verbose = fromFlags.Verbose })
	copyIfChanged(cmd, "no-color", func() { dst.NoColor = fromFlags.NoColor })
	copyIfChanged(cmd, "no-input", func() { dst.NoInput = fromFlags.NoInput })
	copyIfChanged(cmd, "profile", func() { dst.Profile = fromFlags.Profile })
	copyIfChanged(cmd, "config", func() { dst.Config = fromFlags.Config })
	copyIfChanged(cmd, "backend", func() { dst.Backend = fromFlags.Backend })
	copyIfChanged(cmd, "tz", func() { dst.TZ = fromFlags.TZ })
	copyIfChanged(cmd, "schema-version", func() { dst.SchemaVersion = fromFlags.SchemaVersion })

	// If exactly one output mode flag is explicitly set, it overrides env/config output mode.
	modeSet := 0
	if flagValueChanged(cmd, "json") && fromFlags.JSON {
		modeSet++
	}
	if flagValueChanged(cmd, "jsonl") && fromFlags.JSONL {
		modeSet++
	}
	if flagValueChanged(cmd, "plain") && fromFlags.Plain {
		modeSet++
	}
	if modeSet == 1 {
		if flagValueChanged(cmd, "json") && fromFlags.JSON {
			dst.JSON, dst.JSONL, dst.Plain = true, false, false
		}
		if flagValueChanged(cmd, "jsonl") && fromFlags.JSONL {
			dst.JSON, dst.JSONL, dst.Plain = false, true, false
		}
		if flagValueChanged(cmd, "plain") && fromFlags.Plain {
			dst.JSON, dst.JSONL, dst.Plain = false, false, true
		}
	}
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
