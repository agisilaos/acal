package app

import (
	"fmt"
	"time"

	"github.com/agis/acal/internal/contract"
	"github.com/agis/acal/internal/output"
	"github.com/spf13/cobra"
)

func newHistoryCmd(opts *globalOptions) *cobra.Command {
	history := &cobra.Command{Use: "history", Short: "Inspect and undo write history"}

	var limit, offset int
	list := &cobra.Command{
		Use:   "list",
		Short: "List recent history entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, _, err := buildContext(cmd, opts, "history.list")
			if err != nil {
				return err
			}
			paged, hasMore, err := readHistoryPage(limit, offset)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check history file permissions", 1)
			}
			if offset < 0 {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("--offset must be >= 0"), "Use --offset 0 or greater", 2)
			}
			meta := map[string]any{
				"count":       len(paged),
				"limit":       limit,
				"offset":      offset,
				"next_offset": offset + len(paged),
				"has_more":    hasMore,
			}
			if p.EffectiveSuccessMode() == output.ModePlain {
				for _, e := range paged {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", e.At.Format(time.RFC3339), e.Type, e.EventID, e.TxID, e.OpID)
				}
				return nil
			}
			return p.Success(paged, meta, nil)
		},
	}
	list.Flags().IntVar(&limit, "limit", 10, "Maximum entries")
	list.Flags().IntVar(&offset, "offset", 0, "Offset from most recent entry")

	var dryRun bool
	undo := &cobra.Command{
		Use:   "undo",
		Short: "Undo the latest recorded write operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "history.undo")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			entry, meta, err := undoLastHistory(ctx, be, dryRun)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Run `acal history list` to inspect entries", 1)
			}
			if dryRun {
				meta["undone"] = false
			}
			return successWithMeta(ctx, p, ro, entry, meta, nil)
		},
	}
	undo.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview undo without writing")

	redo := &cobra.Command{
		Use:   "redo",
		Short: "Redo the latest undone write operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, be, ro, err := buildContext(cmd, opts, "history.redo")
			if err != nil {
				return err
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			entry, meta, err := redoLastHistory(ctx, be, dryRun)
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Run `acal history undo` first to create redo entries", 1)
			}
			if dryRun {
				meta["redone"] = false
			}
			return successWithMeta(ctx, p, ro, entry, meta, nil)
		},
	}
	redo.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview redo without writing")

	history.AddCommand(list, undo, redo)
	return history
}
