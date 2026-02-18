package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/agis/acal/internal/contract"
	"github.com/spf13/cobra"
)

type statusResult struct {
	Ready         bool                   `json:"ready"`
	Degraded      bool                   `json:"degraded"`
	Backend       string                 `json:"backend"`
	Profile       string                 `json:"profile"`
	TZ            string                 `json:"tz,omitempty"`
	OutputMode    string                 `json:"output_mode"`
	SchemaVersion string                 `json:"schema_version"`
	Checks        []contract.DoctorCheck `json:"checks"`
	NextSteps     []string               `json:"next_steps,omitempty"`
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "acal %s\n", BuildVersionString())
		},
	}
}

func newDoctorCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run preflight checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "doctor")
			if err != nil {
				return err
			}
			_ = ro
			checks, derr := be.Doctor(context.Background())
			meta := map[string]any{"count": len(checks), "ready": derr == nil}
			var warnings []string
			if derr != nil {
				warnings = []string{derr.Error()}
			}
			_ = p.Success(checks, meta, warnings)
			if derr != nil {
				return WrapPrinted(6, derr)
			}
			return nil
		},
	}
}

func newStatusCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show backend health and active runtime configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "status")
			if err != nil {
				return err
			}
			checks, derr := be.Doctor(context.Background())
			setup := buildSetupResult(checks, derr, ro.Backend)
			res := statusResult{
				Ready:         setup.Ready,
				Degraded:      setup.Degraded,
				Backend:       ro.Backend,
				Profile:       ro.Profile,
				TZ:            ro.TZ,
				OutputMode:    string(p.Mode),
				SchemaVersion: ro.SchemaVersion,
				Checks:        checks,
				NextSteps:     setup.NextSteps,
			}
			_ = p.Success(res, map[string]any{
				"ready":    res.Ready,
				"degraded": res.Degraded,
				"checks":   len(res.Checks),
			}, nil)
			if !setup.Ready {
				if derr != nil {
					_ = p.Error(contract.ErrBackendUnavailable, derr.Error(), "Run `acal setup` for remediation")
					return WrapPrinted(6, derr)
				}
				return Wrap(6, fmt.Errorf("status not ready"))
			}
			return nil
		},
	}
}

func newCalendarsCmd(opts *globalOptions) *cobra.Command {
	calendars := &cobra.Command{Use: "calendars", Short: "Calendar resources"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List calendars",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, _, err := buildContext(cmd, opts, "calendars.list")
			if err != nil {
				return err
			}
			items, err := be.ListCalendars(context.Background())
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return WrapPrinted(6, err)
			}
			return p.Success(items, map[string]any{"count": len(items)}, nil)
		},
	}
	calendars.AddCommand(list)
	return calendars
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := strings.ToLower(args[0])
			switch shell {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return Wrap(2, fmt.Errorf("unsupported shell: %s", shell))
			}
		},
	}
}
