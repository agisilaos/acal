package app

import (
	"context"

	"github.com/agis/acal/internal/contract"
	"github.com/spf13/cobra"
)

func newHistoryCmd(opts *globalOptions) *cobra.Command {
	history := &cobra.Command{Use: "history", Short: "Inspect and undo write history"}

	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List recent history entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, _, err := buildContext(cmd, opts, "history.list")
			if err != nil {
				return err
			}
			entries, err := readHistory()
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check history file permissions", 1)
			}
			if limit > 0 && len(entries) > limit {
				entries = entries[len(entries)-limit:]
			}
			return p.Success(entries, map[string]any{"count": len(entries)}, nil)
		},
	}
	list.Flags().IntVar(&limit, "limit", 20, "Maximum entries")

	var dryRun bool
	undo := &cobra.Command{
		Use:   "undo",
		Short: "Undo the latest recorded write operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, _, err := buildContext(cmd, opts, "history.undo")
			if err != nil {
				return err
			}
			entry, meta, err := undoLastHistory(context.Background(), be, dryRun)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Run `acal history list` to inspect entries", 1)
			}
			if dryRun {
				meta["undone"] = false
			}
			return p.Success(entry, meta, nil)
		},
	}
	undo.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview undo without writing")

	redo := &cobra.Command{
		Use:   "redo",
		Short: "Redo the latest undone write operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, _, err := buildContext(cmd, opts, "history.redo")
			if err != nil {
				return err
			}
			entry, meta, err := redoLastHistory(context.Background(), be, dryRun)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Run `acal history undo` first to create redo entries", 1)
			}
			if dryRun {
				meta["redone"] = false
			}
			return p.Success(entry, meta, nil)
		},
	}
	redo.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview redo without writing")

	history.AddCommand(list, undo, redo)
	return history
}
