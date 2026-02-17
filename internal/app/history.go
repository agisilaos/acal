package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type historyEntry struct {
	At      time.Time       `json:"at"`
	Type    string          `json:"type"`
	EventID string          `json:"event_id,omitempty"`
	Prev    *contract.Event `json:"prev,omitempty"`
	Deleted *contract.Event `json:"deleted,omitempty"`
}

func historyFilePath() string {
	base := defaultUserConfigPath()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	dir := filepath.Dir(base)
	return filepath.Join(dir, "history.jsonl")
}

func appendHistory(entry historyEntry) error {
	path := historyFilePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func readHistory() ([]historyEntry, error) {
	path := historyFilePath()
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	out := make([]historyEntry, 0, len(lines))
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		var e historyEntry
		if err := json.Unmarshal([]byte(s), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func writeHistory(entries []historyEntry) error {
	path := historyFilePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func undoLastHistory(ctx context.Context, be backend.Backend, dryRun bool) (historyEntry, map[string]any, error) {
	entries, err := readHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	if len(entries) == 0 {
		return historyEntry{}, nil, fmt.Errorf("history is empty")
	}
	last := entries[len(entries)-1]
	meta := map[string]any{"type": last.Type, "event_id": last.EventID}
	if dryRun {
		meta["dry_run"] = true
		return last, meta, nil
	}
	switch last.Type {
	case "add":
		if strings.TrimSpace(last.EventID) == "" {
			return historyEntry{}, nil, fmt.Errorf("invalid add history entry")
		}
		if err := be.DeleteEvent(ctx, last.EventID, backend.ScopeAuto); err != nil {
			return historyEntry{}, nil, err
		}
	case "delete":
		if last.Deleted == nil {
			return historyEntry{}, nil, fmt.Errorf("invalid delete history entry")
		}
		in := backend.EventCreateInput{
			Calendar: firstNonEmpty(last.Deleted.CalendarName, last.Deleted.CalendarID),
			Title:    last.Deleted.Title,
			Start:    last.Deleted.Start,
			End:      last.Deleted.End,
			Location: last.Deleted.Location,
			Notes:    last.Deleted.Notes,
			URL:      last.Deleted.URL,
			AllDay:   last.Deleted.AllDay,
		}
		if strings.TrimSpace(in.Calendar) == "" {
			return historyEntry{}, nil, fmt.Errorf("deleted entry missing calendar")
		}
		if _, err := be.AddEvent(ctx, in); err != nil {
			return historyEntry{}, nil, err
		}
	case "update":
		if last.Prev == nil {
			return historyEntry{}, nil, fmt.Errorf("invalid update history entry")
		}
		in := backend.EventUpdateInput{Scope: backend.ScopeAuto}
		in.Title = &last.Prev.Title
		in.Start = &last.Prev.Start
		in.End = &last.Prev.End
		in.Location = &last.Prev.Location
		in.Notes = &last.Prev.Notes
		in.URL = &last.Prev.URL
		in.AllDay = &last.Prev.AllDay
		if _, err := be.UpdateEvent(ctx, last.EventID, in); err != nil {
			return historyEntry{}, nil, err
		}
	default:
		return historyEntry{}, nil, fmt.Errorf("unsupported history type: %s", last.Type)
	}
	if err := writeHistory(entries[:len(entries)-1]); err != nil {
		return historyEntry{}, nil, err
	}
	meta["undone"] = true
	return last, meta, nil
}
