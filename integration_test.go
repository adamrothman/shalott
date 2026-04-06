//go:build integration

package main_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adamrothman/shalott/internal/config"
	"github.com/adamrothman/shalott/internal/mirror"
)

func TestIntegration_FullCycle(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up source tree: Artists with two artists, each with albums
	dirs := []string{
		"Artists/Robyn/Honey",
		"Artists/Robyn/Sexistential",
		"Artists/Charlie Puth/Whatever's Clever",
		"Soundtracks/TRON Legacy",
		"Soundtracks/Interstellar",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(sourceDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Set all mtimes to past to pass debounce
	past := time.Now().Add(-10 * time.Minute)
	setMtimeRecursive(t, sourceDir, past)

	cfg := config.Mirror{
		Name:     "music",
		Source:   sourceDir,
		Target:   targetDir,
		Debounce: time.Minute,
		Paths: []config.Path{
			{Prefix: "Artists", Depth: 2},
			{Prefix: "Soundtracks", Depth: 1},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := &mirror.Mirror{Config: cfg, Logger: logger}

	// --- First scan: create all symlinks ---
	result := m.RunOnce(context.Background())
	if result.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", result.Errors)
	}
	if result.Created != 5 {
		t.Fatalf("expected 5 created, got %d", result.Created)
	}

	// Verify symlinks
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Honey"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Honey"))
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Sexistential"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Sexistential"))
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Charlie Puth", "Whatever's Clever"),
		filepath.Join(sourceDir, "Artists", "Charlie Puth", "Whatever's Clever"))
	assertSymlink(t, filepath.Join(targetDir, "Soundtracks", "TRON Legacy"),
		filepath.Join(sourceDir, "Soundtracks", "TRON Legacy"))
	assertSymlink(t, filepath.Join(targetDir, "Soundtracks", "Interstellar"),
		filepath.Join(sourceDir, "Soundtracks", "Interstellar"))

	// Verify intermediate dirs are real directories, not symlinks
	assertRealDir(t, filepath.Join(targetDir, "Artists"))
	assertRealDir(t, filepath.Join(targetDir, "Artists", "Robyn"))
	assertRealDir(t, filepath.Join(targetDir, "Soundtracks"))

	// --- Second scan: no changes ---
	result = m.RunOnce(context.Background())
	if result.Created != 0 || result.Pruned != 0 {
		t.Errorf("expected no changes, got created=%d pruned=%d", result.Created, result.Pruned)
	}

	// --- Remove an album from source, add a new one ---
	os.RemoveAll(filepath.Join(sourceDir, "Artists", "Robyn", "Honey"))
	os.MkdirAll(filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"), 0o755)
	os.Chtimes(filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"), past, past)

	// --- Third scan: should prune Honey, create Body Talk ---
	result = m.RunOnce(context.Background())
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %d", result.Created)
	}
	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}

	// Verify Honey is gone, Body Talk exists
	if _, err := os.Lstat(filepath.Join(targetDir, "Artists", "Robyn", "Honey")); !os.IsNotExist(err) {
		t.Error("Honey symlink should have been pruned")
	}
	assertSymlink(t, filepath.Join(targetDir, "Artists", "Robyn", "Body Talk"),
		filepath.Join(sourceDir, "Artists", "Robyn", "Body Talk"))

	// --- Remove all of an artist's albums ---
	os.RemoveAll(filepath.Join(sourceDir, "Artists", "Charlie Puth"))

	// --- Fourth scan: should prune and clean up empty Charlie Puth dir ---
	result = m.RunOnce(context.Background())
	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "Artists", "Charlie Puth")); !os.IsNotExist(err) {
		t.Error("empty Charlie Puth directory should have been cleaned up")
	}
	// Artists dir should still exist (Robyn is still there)
	assertRealDir(t, filepath.Join(targetDir, "Artists"))
}

func assertSymlink(t *testing.T, linkPath, expectedTarget string) {
	t.Helper()
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Errorf("expected symlink at %s: %v", linkPath, err)
		return
	}
	if target != expectedTarget {
		t.Errorf("symlink %s points to %q, want %q", linkPath, target, expectedTarget)
	}
}

func assertRealDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Errorf("expected directory at %s: %v", path, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s should be a real directory, not a symlink", path)
	}
	if !info.IsDir() {
		t.Errorf("%s should be a directory", path)
	}
}

func setMtimeRecursive(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, mtime, mtime)
	})
}
