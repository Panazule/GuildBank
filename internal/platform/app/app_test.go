package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureWritableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := ensureWritableDir(dir); err != nil {
		t.Fatalf("ensureWritableDir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestEnsureWritableDirFailsForFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "config")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureWritableDir(blocker); err == nil {
		t.Fatal("expected an error when the path is a file")
	}
}
