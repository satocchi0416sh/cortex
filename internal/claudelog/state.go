package claudelog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// currentStateVersion is the on-disk schema version of the claude-log state
// file. Bump on schema-breaking changes; LoadState tolerates 0 (pre-versioning)
// by upgrading it in-place on first save. Counter is independent from the
// markdown sync state version even though both happen to start at 1.
const currentStateVersion = 1

// SessionEntry is the per-session cursor the incremental sync path persists
// between invocations. LastUUID points at the last JSONL entry we successfully
// pushed to Notion; MessageCount is the total number of user/assistant
// messages synced so far (used as a UX figure, not for correctness).
type SessionEntry struct {
	PageID       string    `json:"page_id"`
	LastUUID     string    `json:"last_uuid"`
	MessageCount int       `json:"message_count"`
	LastSynced   time.Time `json:"last_synced"`
}

// State is the disk-backed map of session id → cursor. Concurrent Set / Save
// calls are serialised through mu so a future parallel-sync path stays
// race-free; today's Runner is single-threaded but the mutex is cheap.
type State struct {
	Version  int                     `json:"version"`
	SyncedAt time.Time               `json:"synced_at"`
	Sessions map[string]SessionEntry `json:"sessions"`

	mu   sync.Mutex
	path string
}

// LoadState reads the state file at path. A missing file (or empty file)
// returns a fresh State with version=currentStateVersion and an empty Sessions
// map — the caller must still call Save to materialise it.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{
			Version:  currentStateVersion,
			Sessions: map[string]SessionEntry{},
			path:     path,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read claude-log state: %w", err)
	}
	if len(data) == 0 {
		return &State{
			Version:  currentStateVersion,
			Sessions: map[string]SessionEntry{},
			path:     path,
		}, nil
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse claude-log state: %w", err)
	}
	if s.Sessions == nil {
		s.Sessions = map[string]SessionEntry{}
	}
	if s.Version == 0 {
		s.Version = currentStateVersion
	}
	s.path = path
	return &s, nil
}

// Get returns the entry for sessionID and whether it was present. Safe for
// concurrent use.
func (s *State) Get(sessionID string) (SessionEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Sessions[sessionID]
	return e, ok
}

// Set replaces (or inserts) the entry for sessionID. Call Save to persist.
func (s *State) Set(sessionID string, entry SessionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Sessions == nil {
		s.Sessions = map[string]SessionEntry{}
	}
	s.Sessions[sessionID] = entry
}

// Save atomically writes the state to disk: temp file in the same directory,
// fsync, then rename. Mirrors internal/state.State.Save so both files have the
// same crash-safety story.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SyncedAt = time.Now().UTC()
	if s.Version == 0 {
		s.Version = currentStateVersion
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude-log state: %w", err)
	}

	// 0o755 for the parent dir matches internal/state and the default umask
	// behaviour — owner rwx, others rx, which is what ~/.cortex needs.
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir claude-log state dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".claudelog_state.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
