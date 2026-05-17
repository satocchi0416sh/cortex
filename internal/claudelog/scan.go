package claudelog

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LocateJSONL searches root recursively for a Claude Code session file named
// "<sessionID>.jsonl" and returns the absolute path. When multiple matches
// are found (which can happen if the same session was copied across projects)
// the most-recently-modified file wins and a warning is logged.
func LocateJSONL(root, sessionID string) (string, error) {
	if root == "" {
		return "", errors.New("claude projects root not configured")
	}
	if sessionID == "" {
		return "", errors.New("session id is empty")
	}
	target := sessionID + ".jsonl"

	type candidate struct {
		path    string
		modTime time.Time
	}
	var matches []candidate

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".jsonl") {
			return nil
		}
		if d.Name() != target {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		matches = append(matches, candidate{path: path, modTime: info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk %s: %w", root, walkErr)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no jsonl found for session %s under %s", sessionID, root)
	}
	best := matches[0]
	for _, m := range matches[1:] {
		if m.modTime.After(best.modTime) {
			best = m
		}
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, m := range matches {
			paths = append(paths, m.path)
		}
		slog.Warn("multiple jsonl matches for session, picking newest",
			"session_id", sessionID, "picked", best.path, "candidates", paths)
	}
	return best.path, nil
}

// LocateAllJSONLs walks root recursively and returns every "*.jsonl" file
// path beneath it, sorted lexicographically for deterministic ordering. A
// missing root is treated as "no sessions yet" — a warning is logged and an
// empty slice is returned (so launchd can keep ticking without failing during
// the user's first Claude Code session). An empty root argument is a
// configuration error and surfaces as such.
func LocateAllJSONLs(root string) ([]string, error) {
	if root == "" {
		return nil, errors.New("claude projects root not configured")
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.Warn("claude projects root missing; no sessions to sync", "root", root)
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	var paths []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", root, walkErr)
	}
	sort.Strings(paths)
	return paths, nil
}

// SessionIDFromPath returns the JSONL filename without its ".jsonl" suffix.
// Used by the --all path to derive session IDs from disk without re-parsing.
func SessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
