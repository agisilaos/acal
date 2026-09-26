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

type reminderSnapshot struct {
	Offset *time.Duration `json:"offset_ns"`
}

type historyEntry struct {
	At             time.Time         `json:"at"`
	Type           string            `json:"type"`
	TxID           string            `json:"tx_id,omitempty"`
	OpID           string            `json:"op_id,omitempty"`
	EventID        string            `json:"event_id,omitempty"`
	Prev           *contract.Event   `json:"prev,omitempty"`
	Next           *contract.Event   `json:"next,omitempty"`
	Created        *contract.Event   `json:"created,omitempty"`
	Deleted        *contract.Event   `json:"deleted,omitempty"`
	ReminderBefore *reminderSnapshot `json:"reminder_before,omitempty"`
	ReminderAfter  *reminderSnapshot `json:"reminder_after,omitempty"`
}

// Legacy readers skip undecodable rows. Recognizable reminder payloads fail closed:
// skipping one could make undo replay an older operation.
func decodeHistoryEntry(line string) (*historyEntry, error) {
	var entry historyEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		var tag struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(line), &tag)
		if tag.Type == "reminder" {
			return nil, fmt.Errorf("invalid reminder history entry: %w", err)
		}
		return nil, nil
	}
	if entry.Type == "reminder" && (strings.TrimSpace(entry.EventID) == "" || entry.ReminderBefore == nil || entry.ReminderAfter == nil) {
		return nil, fmt.Errorf("invalid reminder history entry: event id and both reminder snapshots are required")
	}
	return &entry, nil
}

func replayReminder(ctx context.Context, be backend.Backend, id string, snapshot *reminderSnapshot) error {
	patch := backend.EventUpdateInput{Scope: backend.ScopeAuto, ReminderOffset: snapshot.Offset, ClearReminder: snapshot.Offset == nil}
	if _, err := updateEventWithTimeout(ctx, be, id, patch); err != nil {
		return err
	}
	observed, err := reminderOffsetWithTimeout(ctx, be, id)
	if err != nil {
		return &backend.UpdateOutcomeError{Applied: true, Err: fmt.Errorf("reminder verification failed: %w", err)}
	}
	if (observed == nil) != (snapshot.Offset == nil) || (observed != nil && snapshot.Offset != nil && *observed != *snapshot.Offset) {
		return &backend.UpdateOutcomeError{Applied: true, Err: fmt.Errorf("observed reminder offset does not match history")}
	}
	return nil
}

func historyFilePath() string {
	base := defaultUserConfigPath()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	dir := filepath.Dir(base)
	return filepath.Join(dir, "history.jsonl")
}

// openHistoryFile repairs legacy permissions before accessing snapshot contents.
// Only the application directory is tightened, never shared config ancestors.
func openHistoryFile(path string, flag int) (*os.File, error) {
	dir := filepath.Dir(path)
	if flag&os.O_CREATE != 0 {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	// Opening with O_TRUNC would destroy existing snapshots before permission
	// repair succeeds. Chmod the opened file, then truncate that same file.
	f, err := os.OpenFile(path, flag&^os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("history storage is not a regular file: %s", path)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	if flag&os.O_TRUNC != 0 {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func writeHistoryFile(path string, data []byte) error {
	f, err := openHistoryFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func appendHistory(entry historyEntry) error {
	path := historyFilePath()
	if path == "" {
		return nil
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := openHistoryFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
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
	return readHistoryEntries(historyFilePath())
}

func readHistoryEntries(path string) ([]historyEntry, error) {
	if path == "" {
		return nil, nil
	}
	f, err := openHistoryFile(path, os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
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
		e, err := decodeHistoryEntry(s)
		if err != nil {
			return nil, err
		}
		if e != nil {
			out = append(out, *e)
		}
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
	f, err := openHistoryFile(path, os.O_RDONLY)
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
			e, err := decodeHistoryEntry(s)
			if err != nil {
				return nil, false, err
			}
			if e != nil {
				desc = append(desc, *e)
			}
		}
	}
	if pos == 0 {
		s := strings.TrimSpace(remainder)
		if s != "" && len(desc) < need {
			e, err := decodeHistoryEntry(s)
			if err != nil {
				return nil, false, err
			}
			if e != nil {
				desc = append(desc, *e)
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
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return writeHistoryFile(path, []byte(b.String()))
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
	return readHistoryEntries(redoFilePath())
}

func writeRedoHistory(entries []historyEntry) error {
	path := redoFilePath()
	if path == "" {
		return nil
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
	return writeHistoryFile(path, []byte(b.String()))
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
	redoEntries, err := readRedoHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	meta := map[string]any{"type": last.Type, "event_id": last.EventID}
	if dryRun {
		meta["dry_run"] = true
		return last, meta, nil
	}
	redoEntry := last
	switch last.Type {
	case "reminder":
		if err := replayReminder(ctx, be, last.EventID, last.ReminderBefore); err != nil {
			return historyEntry{}, nil, err
		}
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
		id := last.EventID
		if last.Next != nil && last.Next.ID != "" {
			id = last.Next.ID
		}
		updated, err := updateEventWithTimeout(ctx, be, id, in)
		if err != nil {
			return historyEntry{}, nil, err
		}
		redoEntry.EventID = updated.ID
	default:
		return historyEntry{}, nil, fmt.Errorf("unsupported history type: %s", last.Type)
	}
	if err := writeHistory(entries[:len(entries)-1]); err != nil {
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
	historyEntries, err := readHistory()
	if err != nil {
		return historyEntry{}, nil, err
	}
	meta := map[string]any{"type": last.Type, "event_id": last.EventID}
	if dryRun {
		meta["dry_run"] = true
		return last, meta, nil
	}
	applied := last
	switch last.Type {
	case "reminder":
		if err := replayReminder(ctx, be, last.EventID, last.ReminderAfter); err != nil {
			return historyEntry{}, nil, err
		}
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
		updated, err := updateEventWithTimeout(ctx, be, last.EventID, in)
		if err != nil {
			return historyEntry{}, nil, err
		}
		applied.EventID = updated.ID
		applied.Next = updated
	default:
		return historyEntry{}, nil, fmt.Errorf("unsupported redo type: %s", last.Type)
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
