package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultScanRoot(t *testing.T) {
	t.Run("home set", func(t *testing.T) {
		t.Setenv("HOME", "/tmp/home-set-test")
		got := DefaultScanRoot()
		want := filepath.Join("/tmp/home-set-test", "Projects")
		if got != want {
			t.Fatalf("DefaultScanRoot() = %q, want %q", got, want)
		}
	})

	t.Run("home unset", func(t *testing.T) {
		t.Setenv("HOME", "")
		got := DefaultScanRoot()
		if got != "" {
			t.Fatalf("DefaultScanRoot() with empty HOME = %q, want empty string", got)
		}
	})
}
