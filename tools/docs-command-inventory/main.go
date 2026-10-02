// Emit public command metadata for documentation checks without executing a command.
package main

import (
	"encoding/json"
	"os"

	"github.com/agis/acal/internal/app"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type flag struct {
	Shorthand string `json:"shorthand"`
	Type      string `json:"type"`
	NoValue   bool   `json:"no_value"`
}

type command struct {
	Children        map[string]string `json:"children"`
	AcceptsOperands bool              `json:"accepts_operands"`
	Flags           map[string]flag   `json:"flags"`
}

func main() {
	commands := make(map[string]command)
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		// Acal assigns Cobra positional validators to every command. Its bare
		// groups have a help-only RunE, so Runnable alone cannot distinguish them.
		entry := command{Children: make(map[string]string), AcceptsOperands: cmd.ValidateArgs([]string{"docs-operand"}) == nil, Flags: make(map[string]flag)}
		addFlag := func(f *pflag.Flag) {
			entry.Flags[f.Name] = flag{f.Shorthand, f.Value.Type(), f.NoOptDefVal != ""}
		}
		cmd.Flags().VisitAll(addFlag)
		cmd.InheritedFlags().VisitAll(addFlag)
		cmd.PersistentFlags().VisitAll(addFlag)
		// Cobra initializes these built-in flags during execution, which the inventory avoids.
		entry.Flags["help"] = flag{"h", "bool", true}
		if cmd.Version != "" {
			entry.Flags["version"] = flag{"", "bool", true}
		}
		for _, child := range cmd.Commands() {
			if child.Hidden || child.Name() == "help" {
				continue
			}
			entry.Children[child.Name()] = child.Name()
			for _, alias := range child.Aliases {
				entry.Children[alias] = child.Name()
			}
			walk(child)
		}
		commands[cmd.CommandPath()] = entry
	}
	walk(app.NewRootCommand())
	if err := json.NewEncoder(os.Stdout).Encode(commands); err != nil {
		panic(err)
	}
}
