package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	TxID    string          `json:"tx_id,omitempty"`
	OpID    string          `json:"op_id,omitempty"`
	EventID string          `json:"event_id,omitempty"`
	Prev    *contract.Event `json:"prev,omitempty"`
	Next    *contract.Event `json:"next,omitempty"`
	Created *contract.Event `json:"created,omitempty"`
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
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return clearRedoHistory()
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

func readHistoryPage(limit, offset int) ([]historyEntry, bool, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		return nil, false, fmt.Errorf("offset must be >= 0")
	}
	path := historyFilePath()
	if path == "" {
		return nil, false, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() == 0 {
		return nil, false, nil
	}

	need := limit + offset + 1
	desc := make([]historyEntry, 0, need)
	pos := info.Size()
	remainder := ""
	buf := make([]byte, 8192)
	for pos > 0 && len(desc) < need {
		n := int64(len(buf))
		if n > pos {
			n = pos
		}
		pos -= n
		if _, err := f.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
			return nil, false, err
		}
		chunk := string(buf[:n]) + remainder
		parts := strings.Split(chunk, "\n")
		remainder = parts[0]
		for i := len(parts) - 1; i >= 1 && len(desc) < need; i-- {
			s := strings.TrimSpace(parts[i])
			if s == "" {
				continue
			}
			var e historyEntry
			if err := json.Unmarshal([]byte(s), &e); err != nil {
				continue
			}
			desc = append(desc, e)
		}
	}
	if pos == 0 {
		s := strings.TrimSpace(remainder)
		if s != "" && len(desc) < need {
			var e historyEntry
			if err := json.Unmarshal([]byte(s), &e); err == nil {
				desc = append(desc, e)
			}
		}
	}

	if len(desc) <= offset {
		return nil, false, nil
	}
	end := offset + limit
	if end > len(desc) {
		end = len(desc)
	}
	slice := desc[offset:end]
	out := make([]historyEntry, 0, len(slice))
	for i := len(slice) - 1; i >= 0; i-- {
		out = append(out, slice[i])
	}
	hasMore := len(desc) > end
	return out, hasMore, nil
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

func redoFilePath() string {
	base := defaultUserConfigPath()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	dir := filepath.Dir(base)
	return filepath.Join(dir, "redo.jsonl")
}

func readRedoHistory() ([]historyEntry, error) {
	path := redoFilePath()
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

func writeRedoHistory(entries []historyEntry) error {
	path := redoFilePath()
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

func clearRedoHistory() error {
	return writeRedoHistory(nil)
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
	redoEntry := last
	switch last.Type {
	case "add":
		if strings.TrimSpace(last.EventID) == "" {
			return historyEntry{}, nil, fmt.Errorf("invalid add history entry")
		}
		if err := deleteEventWithTimeout(ctx, be, last.EventID, backend.ScopeAuto); err != nil {
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
		created, err := addEventWithTimeout(ctx, be, in)
		if err != nil {
			return historyEntry{}, nil, err
		}
		if created != nil {
			redoEntry.EventID = created.ID
			redoEntry.Deleted = created
		}
	case "update":
		if last.Prev == nil {
			return historyEntry{}, nil, fmt.Errorf("invalid update history entry")
		}
		in := buildUpdateInputFromEvent(last.Prev)
		if _, err := updateEventWithTimeout(ctx, be, last.EventID, in); err != nil {
			return historyEntry{}, nil, err
		}
	default:
		return historyEntry{}, nil, fmt.Errorf("unsupported history type: %s", last.Type)
	}
	if err := writeHistory(entries[:len(entries)-1]); err != nil {
		return historyEntry{}, nil, err
	}
	redoEntries, err := readRedoHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	redoEntry.At = time.Now().UTC()
	redoEntries = append(redoEntries, redoEntry)
	if err := writeRedoHistory(redoEntries); err != nil {
		return historyEntry{}, nil, err
	}
	meta["undone"] = true
	return last, meta, nil
}

func redoLastHistory(ctx context.Context, be backend.Backend, dryRun bool) (historyEntry, map[string]any, error) {
	redoEntries, err := readRedoHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	if len(redoEntries) == 0 {
		return historyEntry{}, nil, fmt.Errorf("redo history is empty")
	}
	last := redoEntries[len(redoEntries)-1]
	meta := map[string]any{"type": last.Type, "event_id": last.EventID}
	if dryRun {
		meta["dry_run"] = true
		return last, meta, nil
	}
	applied := last
	switch last.Type {
	case "add":
		if last.Created == nil {
			return historyEntry{}, nil, fmt.Errorf("add redo requires created snapshot")
		}
		in := backend.EventCreateInput{
			Calendar:       firstNonEmpty(last.Created.CalendarName, last.Created.CalendarID),
			Title:          last.Created.Title,
			Start:          last.Created.Start,
			End:            last.Created.End,
			Location:       last.Created.Location,
			Notes:          last.Created.Notes,
			URL:            last.Created.URL,
			AllDay:         last.Created.AllDay,
			ReminderOffset: nil,
			RepeatRule:     "",
		}
		created, err := addEventWithTimeout(ctx, be, in)
		if err != nil {
			return historyEntry{}, nil, err
		}
		if created != nil {
			applied.EventID = created.ID
			applied.Created = created
		}
	case "delete":
		if strings.TrimSpace(last.EventID) == "" {
			return historyEntry{}, nil, fmt.Errorf("delete redo missing event id")
		}
		if err := deleteEventWithTimeout(ctx, be, last.EventID, backend.ScopeAuto); err != nil {
			return historyEntry{}, nil, err
		}
	case "update":
		if last.Next == nil {
			return historyEntry{}, nil, fmt.Errorf("update redo requires next snapshot")
		}
		in := buildUpdateInputFromEvent(last.Next)
		if _, err := updateEventWithTimeout(ctx, be, last.EventID, in); err != nil {
			return historyEntry{}, nil, err
		}
	default:
		return historyEntry{}, nil, fmt.Errorf("unsupported redo type: %s", last.Type)
	}
	historyEntries, err := readHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	applied.At = time.Now().UTC()
	historyEntries = append(historyEntries, applied)
	if err := writeHistory(historyEntries); err != nil {
		return historyEntry{}, nil, err
	}
	if err := writeRedoHistory(redoEntries[:len(redoEntries)-1]); err != nil {
		return historyEntry{}, nil, err
	}
	meta["redone"] = true
	return applied, meta, nil
}

func buildUpdateInputFromEvent(ev *contract.Event) backend.EventUpdateInput {
	if ev == nil {
		return backend.EventUpdateInput{Scope: backend.ScopeAuto}
	}
	in := backend.EventUpdateInput{Scope: backend.ScopeAuto}
	in.Title = &ev.Title
	in.Start = &ev.Start
	in.End = &ev.End
	in.Location = &ev.Location
	in.Notes = &ev.Notes
	in.URL = &ev.URL
	in.AllDay = &ev.AllDay
	return in
}
