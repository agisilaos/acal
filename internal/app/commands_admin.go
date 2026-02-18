package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
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
	ReasonCodes   []string               `json:"degraded_reason_codes,omitempty"`
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
			ctx, cancel := commandContext(ro)
			defer cancel()
			checks, derr := doctorWithTimeout(ctx, be)
			setup := buildSetupResult(checks, derr, ro.Backend)
			reasonCodes := deriveDegradedReasonCodes(checks, derr)
			meta := map[string]any{
				"count":                 len(checks),
				"ready":                 setup.Ready,
				"degraded":              setup.Degraded,
				"degraded_reason_codes": reasonCodes,
			}
			warnings := setup.Notes
			if p.EffectiveSuccessMode() == output.ModePlain {
				return printDoctorPlain(cmd.OutOrStdout(), checks, setup, reasonCodes)
			}
			_ = successWithMeta(ctx, p, ro, checks, meta, warnings)
			if !setup.Ready && derr != nil {
				return WrapPrinted(6, derr)
			}
			if !setup.Ready {
				return Wrap(6, fmt.Errorf("doctor checks not ready"))
			}
			return nil
		},
	}
}

func newStatusCmd(opts *globalOptions) *cobra.Command {
	status := &cobra.Command{
		Use:   "status",
		Short: "Show backend health and active runtime configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "status")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			checks, derr := doctorWithTimeout(ctx, be)
			setup := buildSetupResult(checks, derr, ro.Backend)
			reasonCodes := deriveDegradedReasonCodes(checks, derr)
			res := statusResult{
				Ready:         setup.Ready,
				Degraded:      setup.Degraded,
				Backend:       ro.Backend,
				Profile:       ro.Profile,
				TZ:            ro.TZ,
				OutputMode:    string(p.EffectiveSuccessMode()),
				SchemaVersion: ro.SchemaVersion,
				Checks:        checks,
				NextSteps:     setup.NextSteps,
				ReasonCodes:   reasonCodes,
			}
			meta := map[string]any{
				"ready":                 res.Ready,
				"degraded":              res.Degraded,
				"checks":                len(res.Checks),
				"degraded_reason_codes": reasonCodes,
			}
			if p.EffectiveSuccessMode() == output.ModePlain {
				_ = printStatusPlain(cmd.OutOrStdout(), res)
			} else {
				_ = successWithMeta(ctx, p, ro, res, meta, nil)
			}
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
	explain := &cobra.Command{
		Use:   "explain",
		Short: "Explain current health state and remediation steps",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "status.explain")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			checks, derr := doctorWithTimeout(ctx, be)
			setup := buildSetupResult(checks, derr, ro.Backend)
			reasons := deriveDegradedReasonCodes(checks, derr)
			if p.EffectiveSuccessMode() == output.ModePlain {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ready=%t degraded=%t\n", setup.Ready, setup.Degraded)
				if len(reasons) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reasons=%s\n", strings.Join(reasons, ","))
				}
				for _, s := range setup.NextSteps {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", s)
				}
			} else {
				_ = successWithMeta(ctx, p, ro, map[string]any{
					"ready":                 setup.Ready,
					"degraded":              setup.Degraded,
					"degraded_reason_codes": reasons,
					"next_steps":            setup.NextSteps,
				}, map[string]any{"count": len(setup.NextSteps)}, setup.Notes)
			}
			if !setup.Ready && derr != nil {
				return WrapPrinted(6, derr)
			}
			if !setup.Ready {
				return Wrap(6, fmt.Errorf("status not ready"))
			}
			return nil
		},
	}
	status.AddCommand(explain)
	return status
}

func newCalendarsCmd(opts *globalOptions) *cobra.Command {
	calendars := &cobra.Command{Use: "calendars", Short: "Calendar resources"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List calendars",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "calendars.list")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			items, err := listCalendarsWithTimeout(ctx, be)
			if err != nil {
				_ = p.Error(contract.ErrBackendUnavailable, err.Error(), "Run `acal doctor` for remediation")
				return WrapPrinted(6, err)
			}
			return successWithMeta(ctx, p, ro, items, map[string]any{"count": len(items)}, nil)
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

func deriveDegradedReasonCodes(checks []contract.DoctorCheck, derr error) []string {
	codeSet := map[string]struct{}{}
	for _, c := range checks {
		status := strings.ToLower(strings.TrimSpace(c.Status))
		if status == "" || status == "ok" || status == "pass" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(c.Name))
		name = strings.ReplaceAll(name, " ", "_")
		name = strings.ReplaceAll(name, "-", "_")
		if name == "" {
			name = "unknown_check"
		}
		codeSet[name+"_fail"] = struct{}{}
	}
	if derr != nil {
		codeSet["doctor_error"] = struct{}{}
	}
	if len(codeSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(codeSet))
	for code := range codeSet {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func printDoctorPlain(out io.Writer, checks []contract.DoctorCheck, setup setupResult, reasonCodes []string) error {
	_, _ = fmt.Fprintf(out, "ready=%t degraded=%t checks=%d\n", setup.Ready, setup.Degraded, len(checks))
	if len(reasonCodes) > 0 {
		_, _ = fmt.Fprintf(out, "reasons=%s\n", strings.Join(reasonCodes, ","))
	}
	for _, c := range checks {
		_, _ = fmt.Fprintf(out, "[%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	for _, step := range setup.NextSteps {
		_, _ = fmt.Fprintf(out, "next: %s\n", step)
	}
	return nil
}

func printStatusPlain(out io.Writer, res statusResult) error {
	_, _ = fmt.Fprintf(out, "ready=%t degraded=%t backend=%s profile=%s output_mode=%s checks=%d\n", res.Ready, res.Degraded, res.Backend, res.Profile, res.OutputMode, len(res.Checks))
	if len(res.ReasonCodes) > 0 {
		_, _ = fmt.Fprintf(out, "reasons=%s\n", strings.Join(res.ReasonCodes, ","))
	}
	for _, c := range res.Checks {
		_, _ = fmt.Fprintf(out, "[%s] %s: %s\n", c.Status, c.Name, c.Message)
	}
	return nil
}
