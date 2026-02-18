package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agis/acal/internal/contract"
	"github.com/spf13/cobra"
)

type savedQuery struct {
	Name      string   `json:"name"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Calendars []string `json:"calendars,omitempty"`
	Wheres    []string `json:"wheres,omitempty"`
	Sort      string   `json:"sort,omitempty"`
	Order     string   `json:"order,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

func queriesFilePath() string {
	base := defaultUserConfigPath()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(base), "queries.json")
}

func loadSavedQueries() (map[string]savedQuery, error) {
	path := queriesFilePath()
	if path == "" {
		return map[string]savedQuery{}, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]savedQuery{}, nil
	}
	if err != nil {
		return nil, err
	}
	store := map[string]savedQuery{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, err
	}
	return store, nil
}

func writeSavedQueries(store map[string]savedQuery) error {
	path := queriesFilePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func newQueriesCmd(opts *globalOptions) *cobra.Command {
	queries := &cobra.Command{Use: "queries", Short: "Saved query presets"}

	var saveFrom, saveTo, saveSort, saveOrder string
	var saveCalendars, saveWheres []string
	var saveLimit int
	var overwrite bool
	save := &cobra.Command{
		Use:   "save <name>",
		Short: "Save a query preset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, _, err := buildContext(cmd, opts, "queries.save")
			if err != nil {
				return err
			}
			name := strings.TrimSpace(args[0])
			if name == "" {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("name is required"), "Provide a preset name", 2)
			}
			store, err := loadSavedQueries()
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check queries file permissions", 1)
			}
			if _, exists := store[name]; exists && !overwrite {
				return failWithHint(p, contract.ErrInvalidUsage, fmt.Errorf("query already exists: %s", name), "Use --overwrite to replace existing query", 2)
			}
			store[name] = savedQuery{Name: name, From: saveFrom, To: saveTo, Calendars: saveCalendars, Wheres: saveWheres, Sort: saveSort, Order: saveOrder, Limit: saveLimit}
			if err := writeSavedQueries(store); err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Unable to persist query preset", 1)
			}
			return p.Success(store[name], map[string]any{"saved": true}, nil)
		},
	}
	save.Flags().StringVar(&saveFrom, "from", "today", "Range start")
	save.Flags().StringVar(&saveTo, "to", "+30d", "Range end")
	save.Flags().StringSliceVar(&saveCalendars, "calendar", nil, "Calendar ID or name (repeatable)")
	save.Flags().StringSliceVar(&saveWheres, "where", nil, "Predicate clause (repeatable)")
	save.Flags().StringVar(&saveSort, "sort", "start", "Sort field")
	save.Flags().StringVar(&saveOrder, "order", "asc", "Sort order: asc|desc")
	save.Flags().IntVar(&saveLimit, "limit", 0, "Limit results")
	save.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing preset")

	list := &cobra.Command{
		Use:   "list",
		Short: "List saved queries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, _, err := buildContext(cmd, opts, "queries.list")
			if err != nil {
				return err
			}
			store, err := loadSavedQueries()
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check queries file permissions", 1)
			}
			rows := make([]savedQuery, 0, len(store))
			for _, q := range store {
				rows = append(rows, q)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
			return p.Success(rows, map[string]any{"count": len(rows)}, nil)
		},
	}

	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, _, err := buildContext(cmd, opts, "queries.delete")
			if err != nil {
				return err
			}
			store, err := loadSavedQueries()
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check queries file permissions", 1)
			}
			name := strings.TrimSpace(args[0])
			if _, ok := store[name]; !ok {
				return failWithHint(p, contract.ErrNotFound, fmt.Errorf("query not found: %s", name), "Run `acal queries list` to inspect names", 4)
			}
			delete(store, name)
			if err := writeSavedQueries(store); err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Unable to persist query store", 1)
			}
			return p.Success(map[string]any{"deleted": true, "name": name}, map[string]any{"count": 1}, nil)
		},
	}

	run := &cobra.Command{
		Use:   "run <name>",
		Short: "Run a saved query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, be, ro, err := buildContext(cmd, opts, "queries.run")
			if err != nil {
				return err
			}
			store, err := loadSavedQueries()
			if err != nil {
				return failWithHint(p, contract.ErrGeneric, err, "Check queries file permissions", 1)
			}
			q, ok := store[strings.TrimSpace(args[0])]
			if !ok {
				return failWithHint(p, contract.ErrNotFound, fmt.Errorf("query not found: %s", args[0]), "Run `acal queries list`", 4)
			}
			f, err := buildEventFilterWithTZ(q.From, q.To, q.Calendars, q.Limit, ro.TZ)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Saved query has invalid range; re-save it", 2)
			}
			ctx, cancel := commandContext(ro)
			defer cancel()
			items, err := listEventsWithTimeout(ctx, be, f)
			if err != nil {
				return failWithHint(p, contract.ErrBackendUnavailable, err, "Run `acal doctor` for remediation", 6)
			}
			preds, err := parsePredicates(q.Wheres)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Saved query has invalid predicates; re-save it", 2)
			}
			items, err = applyPredicates(items, preds)
			if err != nil {
				return failWithHint(p, contract.ErrInvalidUsage, err, "Saved query predicates failed; re-save it", 2)
			}
			sortEvents(items, q.Sort, q.Order)
			if q.Limit > 0 && len(items) > q.Limit {
				items = items[:q.Limit]
			}
			return p.Success(items, map[string]any{"count": len(items), "name": q.Name}, nil)
		},
	}

	queries.AddCommand(save, list, del, run)
	return queries
}
