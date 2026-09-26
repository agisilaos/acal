package app

import "github.com/spf13/cobra"

// Command lookup precedes Cobra's validation hooks. Classify only errors from
// that phase; errors returned by command execution keep their original codes.
func executeCommand(root *cobra.Command, args []string) error {
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		// Execute initializes help and shell-completion commands before lookup.
		// Inspect the same initialized tree so those commands remain valid.
		if _, _, lookupErr := root.Find(args); lookupErr != nil {
			return Wrap(2, err)
		}
	}
	return err
}

func classifyUsageErrors(cmd *cobra.Command) {
	// Every command declares its positional contract. Bare groups still show
	// help, but must validate unknown subcommands before doing so.
	if cmd.Args == nil {
		cmd.Args = cobra.NoArgs
	}
	if !cmd.Runnable() && cmd.HasSubCommands() {
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return Wrap(2, err)
	})
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			return Wrap(2, validate(cmd, args))
		}
	}
	for _, child := range cmd.Commands() {
		classifyUsageErrors(child)
	}
}
