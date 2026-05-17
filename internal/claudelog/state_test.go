package claudelog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLoadState_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does", "not", "exist.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(s.Sessions) != 0 {
		t.Errorf("expected empty Sessions, got %d", len(s.Sessions))
	}
	if s.Version != currentStateVersion {
		t.Errorf("Version = %d, want %d", s.Version, currentStateVersion)
	}
}

func TestLoadState_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s.Set("sess-1", SessionEntry{
		PageID:       "page-1",
		LastUUID:     "uuid-5",
		MessageCount: 5,
		LastSynced:   now,
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState reload: %v", err)
	}
	got, ok := loaded.Get("sess-1")
	if !ok {
		t.Fatal("entry missing after roundtrip")
	}
	if got.PageID != "page-1" || got.LastUUID != "uuid-5" || got.MessageCount != 5 {
		t.Errorf("entry data lost: %+v", got)
	}
	if !got.LastSynced.Equal(now) {
		t.Errorf("time mismatch: got=%v want=%v", got.LastSynced, now)
	}
}

func TestState_GetUnknown(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("expected ok=false for unknown session")
	}
}

func TestState_ConcurrentSet(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Set(fmt.Sprintf("sess-%d", i), SessionEntry{
				PageID:       fmt.Sprintf("page-%d", i),
				LastUUID:     fmt.Sprintf("uuid-%d", i),
				MessageCount: i,
				LastSynced:   time.Now(),
			})
		}(i)
	}
	wg.Wait()
	if err := s.Save(); err != nil {
		t.Fatalf("Save after concurrent Set: %v", err)
	}
}

func TestLoadState_VersionPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	s.Set("sess-x", SessionEntry{PageID: "p", LastUUID: "u", MessageCount: 1})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.Version != currentStateVersion {
		t.Errorf("on-disk Version = %d, want %d", raw.Version, currentStateVersion)
	}
}
