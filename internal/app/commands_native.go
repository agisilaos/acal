package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agis/acal/internal/nativeproof"
	"github.com/spf13/cobra"
)

// This opt-in proof intentionally does not use production config or history.
func newNativeCmd(opts *globalOptions) *cobra.Command {
	var state string
	root := &cobra.Command{Use: "native", Short: "Experimental packaged EventKit proof (JSON, owned fixtures only)", Long: "Experimental native proof. Always emits versioned JSON; ignores configuration files and ACAL_* environment. Writes require an isolated absolute --state-dir and only modify fixtures created with that state. Production undo/redo is not supported."}
	root.PersistentFlags().StringVar(&state, "state-dir", "", "Absolute directory for isolated fixture ownership and durable operation records")
	run := func(op string, params func(*cobra.Command, []string) (map[string]any, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			values, e := params(cmd, args)
			if e != nil {
				return Wrap(2, e)
			}
			for _, name := range []string{"plain", "jsonl", "fields", "config", "profile", "backend", "tz", "schema-version", "quiet", "fail-on-degraded"} {
				if cmd.Flags().Changed(name) {
					return Wrap(2, fmt.Errorf("--%s is not supported by the isolated native proof", name))
				}
			}
			if opts.Timeout < 0 {
				return Wrap(2, errors.New("timeout must not be negative"))
			}
			if op == "setup" && values["request_access"] == true && opts.NoInput {
				return Wrap(2, errors.New("--request-access cannot be used with --no-input"))
			}
			helper, e := nativeproof.Installed()
			if e != nil {
				return Wrap(6, e)
			}
			ctx := cmd.Context()
			var cancel context.CancelFunc
			if opts.Timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
				defer cancel()
			}
			res, e := (nativeproof.Client{Helper: helper, StateDir: state}).Call(ctx, op, values)
			if res.Protocol != "" {
				if outErr := json.NewEncoder(cmd.OutOrStdout()).Encode(res); outErr != nil {
					return Wrap(1, outErr)
				}
			}
			if e != nil {
				if res.Protocol != "" {
					return WrapPrinted(6, e)
				}
				return Wrap(2, e)
			}
			return nil
		}
	}
	empty := func(*cobra.Command, []string) (map[string]any, error) { return map[string]any{}, nil }
	setup := &cobra.Command{Use: "setup", Short: "Check Full Calendar Access; prompt only with --request-access", Args: cobra.NoArgs}
	setup.Flags().Bool("request-access", false, "Explicitly request Full Calendar Access (may show a macOS prompt)")
	setup.RunE = run("setup", func(c *cobra.Command, _ []string) (map[string]any, error) {
		v, _ := c.Flags().GetBool("request-access")
		return map[string]any{"request_access": v}, nil
	})
	root.AddCommand(setup)
	root.AddCommand(&cobra.Command{Use: "calendars", Short: "List native calendar IDs, sources and writability", Args: cobra.NoArgs, RunE: run("calendars", empty)})
	events := &cobra.Command{Use: "events", Short: "Read events and mutate only this proof's independent fixtures"}
	root.AddCommand(events)
	list := &cobra.Command{Use: "list", Short: "List occurrences in an explicit half-open interval (at most four years)", Args: cobra.NoArgs}
	list.Flags().String("from", "", "RFC3339 start instant (required)")
	list.Flags().String("to", "", "RFC3339 exclusive end instant (required)")
	list.Flags().String("calendar", "", "Exact native calendar ID (required)")
	list.Flags().Int("limit", 100, "Maximum results, 1–1000; truncation is reported")
	list.RunE = run("list", func(c *cobra.Command, _ []string) (map[string]any, error) {
		v := map[string]any{}
		for _, k := range []string{"from", "to", "calendar"} {
			s, _ := c.Flags().GetString(k)
			if s == "" {
				return nil, fmt.Errorf("--%s is required", k)
			}
			v[k] = s
		}
		n, _ := c.Flags().GetInt("limit")
		v["limit"] = n
		return v, nil
	})
	events.AddCommand(list)
	add := &cobra.Command{Use: "add", Short: "Create a uniquely marked timed fixture with an optional reminder", Args: cobra.NoArgs}
	for _, k := range []string{"calendar", "title", "start", "end"} {
		add.Flags().String(k, "", k+" (required; dates are RFC3339 instants)")
	}
	add.Flags().Duration("before", 0, "Display reminder before start (0 means at start; omitted means none)")
	add.RunE = run("add", func(c *cobra.Command, _ []string) (map[string]any, error) {
		v := map[string]any{}
		for _, k := range []string{"calendar", "title", "start", "end"} {
			s, _ := c.Flags().GetString(k)
			if s == "" {
				return nil, fmt.Errorf("--%s is required", k)
			}
			v[k] = s
		}
		if c.Flags().Changed("before") {
			d, _ := c.Flags().GetDuration("before")
			if d < 0 || d%time.Minute != 0 {
				return nil, errors.New("--before must be nonnegative whole minutes")
			}
			v["alarm_seconds"] = -d.Seconds()
		}
		return v, nil
	})
	events.AddCommand(add)
	show := &cobra.Command{Use: "show ID", Short: "Read one exact native event reference", Args: cobra.ExactArgs(1), RunE: run("show", func(_ *cobra.Command, a []string) (map[string]any, error) { return map[string]any{"id": a[0]}, nil })}
	events.AddCommand(show)
	update := &cobra.Command{Use: "update ID", Short: "Change an owned fixture's title", Args: cobra.ExactArgs(1)}
	update.Flags().String("title", "", "New title (required)")
	update.RunE = run("update", func(c *cobra.Command, a []string) (map[string]any, error) {
		s, _ := c.Flags().GetString("title")
		if s == "" {
			return nil, errors.New("--title is required")
		}
		return map[string]any{"id": a[0], "title": s}, nil
	})
	events.AddCommand(update)
	remind := &cobra.Command{Use: "remind ID", Short: "Replace all display alarms, or clear them, on an owned fixture", Args: cobra.ExactArgs(1)}
	remind.Flags().Duration("before", 0, "Nonnegative whole minutes before start (0 means at start)")
	remind.Flags().Bool("clear", false, "Clear display alarms")
	remind.RunE = run("remind", func(c *cobra.Command, a []string) (map[string]any, error) {
		clear, _ := c.Flags().GetBool("clear")
		before := c.Flags().Changed("before")
		if clear == before {
			return nil, errors.New("specify exactly one of --before or --clear")
		}
		v := map[string]any{"id": a[0], "clear": clear}
		if before {
			d, _ := c.Flags().GetDuration("before")
			if d < 0 || d%time.Minute != 0 {
				return nil, errors.New("--before must be nonnegative whole minutes")
			}
			v["alarm_seconds"] = -d.Seconds()
		}
		return v, nil
	})
	events.AddCommand(remind)
	events.AddCommand(&cobra.Command{Use: "delete ID", Short: "Delete one exact owned fixture; no production undo is available", Args: cobra.ExactArgs(1), RunE: run("delete", func(_ *cobra.Command, a []string) (map[string]any, error) { return map[string]any{"id": a[0]}, nil })})
	return root
}
