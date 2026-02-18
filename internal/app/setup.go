package app

import (
	"strings"

	"github.com/agis/acal/internal/contract"
	"github.com/spf13/cobra"
)

type setupResult struct {
	Ready      bool                   `json:"ready"`
	Degraded   bool                   `json:"degraded"`
	Checks     []contract.DoctorCheck `json:"checks"`
	NextSteps  []string               `json:"next_steps,omitempty"`
	Notes      []string               `json:"notes,omitempty"`
	Backend    string                 `json:"backend"`
	Permission string                 `json:"permission_model"`
}

func newSetupCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Run first-time setup checks and permission guidance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "setup")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			checks, derr := doctorWithTimeout(ctx, be)
			res := buildSetupResult(checks, derr, ro.Backend)
			_ = p.Success(res, map[string]any{
				"ready":    res.Ready,
				"degraded": res.Degraded,
				"count":    len(checks),
			}, nil)
			if res.Ready {
				return nil
			}
			if derr != nil {
				_ = p.Error(contract.ErrBackendUnavailable, derr.Error(), "Run `acal setup` again after applying next_steps")
				return WrapPrinted(6, derr)
			}
			return Wrap(6, err)
		},
	}
}

func buildSetupResult(checks []contract.DoctorCheck, derr error, backend string) setupResult {
	res := setupResult{
		Ready:      true,
		Degraded:   false,
		Checks:     checks,
		Backend:    strings.TrimSpace(backend),
		Permission: "macOS TCC stores approvals per app identity; keep a stable terminal and acal install path",
	}

	has := func(name string) (string, bool) {
		for _, c := range checks {
			if strings.EqualFold(strings.TrimSpace(c.Name), name) {
				return strings.ToLower(strings.TrimSpace(c.Status)), true
			}
		}
		return "", false
	}

	osascriptStatus, hasOsa := has("osascript")
	accessStatus, hasAccess := has("calendar_access")
	dbReadStatus, hasDBRead := has("calendar_db_read")
	dbStatus, hasDB := has("calendar_db")

	if !hasOsa || osascriptStatus != "ok" {
		res.Ready = false
		res.NextSteps = append(res.NextSteps, "Install or expose `osascript` in PATH (default on macOS).")
	}
	if !hasAccess || accessStatus != "ok" {
		res.Ready = false
		res.NextSteps = append(res.NextSteps, "Grant Calendar automation permission for your terminal app in System Settings > Privacy & Security > Automation.")
	}
	if hasDBRead && dbReadStatus != "ok" {
		res.Degraded = true
		res.Notes = append(res.Notes, "Calendar database reads are unavailable; acal may run in slower/degraded mode.")
		res.NextSteps = append(res.NextSteps, "Optional: grant Full Disk Access to your terminal for faster/stabler DB-based reads.")
	}
	if hasDB && dbStatus != "ok" {
		res.Degraded = true
		res.Notes = append(res.Notes, "Calendar DB path was not detected.")
	}

	if res.Ready {
		res.NextSteps = append(res.NextSteps, "Verify read access with: `acal today --json`")
		res.NextSteps = append(res.NextSteps, "Verify write access with: `acal quick-add \"tomorrow 10:00 Test @Personal 30m\" --dry-run --json`")
	}

	if derr != nil && !res.Ready {
		res.Notes = append(res.Notes, derr.Error())
	}
	return res
}
