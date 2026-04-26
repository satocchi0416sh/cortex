package state

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

const currentVersion = 1

type FileEntry struct {
	AbsPath     string    `json:"abs_path"`
	Project     string    `json:"project"`
	RelPath     string    `json:"rel_path"`
	Filename    string    `json:"filename"`
	ContentHash string    `json:"content_hash"`
	PageID      string    `json:"page_id"`
	LastSynced  time.Time `json:"last_synced"`
}

type State struct {
	Version  int                  `json:"version"`
	SyncedAt time.Time            `json:"synced_at"`
	Files    map[string]FileEntry `json:"files"`

	mu   sync.Mutex
	path string
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{
			Version: currentVersion,
			Files:   map[string]FileEntry{},
			path:    path,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return &State{
			Version: currentVersion,
			Files:   map[string]FileEntry{},
			path:    path,
		}, nil
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Files == nil {
		s.Files = map[string]FileEntry{}
	}
	if s.Version == 0 {
		s.Version = currentVersion
	}
	s.path = path
	return &s, nil
}

func (s *State) Get(externalID string) (FileEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Files[externalID]
	return e, ok
}

func (s *State) Set(externalID string, e FileEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Files[externalID] = e
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SyncedAt = time.Now().UTC()
	if s.Version == 0 {
		s.Version = currentVersion
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sync_state.*.tmp")
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
