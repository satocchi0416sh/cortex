package claudelog

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
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
