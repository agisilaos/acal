package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/agis/acal/internal/contract"
	"github.com/spf13/cobra"
)

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
			_ = p.Success(checks, map[string]any{"count": len(checks)}, nil)
			if derr != nil {
				_ = p.Error(contract.ErrBackendUnavailable, derr.Error(), "Run with GUI session and grant Calendar automation permission")
				return Wrap(6, derr)
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
				return Wrap(6, err)
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
