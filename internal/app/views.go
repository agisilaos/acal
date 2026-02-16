package app

import "github.com/spf13/cobra"

func newViewCmd(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "View events in common calendar ranges",
	}
	cmd.AddCommand(newTodayCmd(opts))
	cmd.AddCommand(newWeekCmd(opts))
	cmd.AddCommand(newMonthCmd(opts))
	return cmd
}
