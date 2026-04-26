package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type File struct {
	AbsPath     string
	Project     string
	RelPath     string
	Filename    string
	ExternalID  string
	ContentHash string
	Body        []byte
}

func Scan(root, pattern string) ([]File, error) {
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat scan root: %w", err)
	}

	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}

	out := make([]File, 0, len(matches))
	for _, abs := range matches {
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		f, err := build(root, abs)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func build(root, abs string) (File, error) {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return File{}, fmt.Errorf("rel path: %w", err)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return File{}, fmt.Errorf("unexpected path layout: %s", rel)
	}
	project := parts[0]
	relWithin := strings.Join(parts[1:], "/")

	body, err := os.ReadFile(abs)
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", abs, err)
	}

	contentSum := sha256.Sum256(body)
	idSrc := project + "/" + relWithin
	idSum := sha256.Sum256([]byte(idSrc))

	return File{
		AbsPath:     abs,
		Project:     project,
		RelPath:     relWithin,
		Filename:    filepath.Base(abs),
		ExternalID:  hex.EncodeToString(idSum[:]),
		ContentHash: hex.EncodeToString(contentSum[:]),
		Body:        body,
	}, nil
}

func TitleFromFilename(name string) string {
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	return strings.TrimSuffix(base, ext)
}
