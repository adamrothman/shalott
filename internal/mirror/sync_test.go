package mirror_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/adamrothman/shalott/internal/mirror"
)

func TestDiff_AllNew(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey", "Robyn/Sexistential"})

	scanned := []string{
		filepath.Join(sourceDir, "Robyn", "Honey"),
		filepath.Join(sourceDir, "Robyn", "Sexistential"),
	}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 2 {
		t.Fatalf("expected 2 new entries, got %d", len(result.New))
	}
	if len(result.Orphan) != 0 {
		t.Errorf("expected 0 orphans, got %d", len(result.Orphan))
	}
	if len(result.Valid) != 0 {
		t.Errorf("expected 0 valid, got %d", len(result.Valid))
	}
}

func TestDiff_AllValid(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey"})

	// Create matching symlink in target
	mkdirs(t, targetDir, []string{"Robyn"})
	os.Symlink(
		filepath.Join(sourceDir, "Robyn", "Honey"),
		filepath.Join(targetDir, "Robyn", "Honey"),
	)

	scanned := []string{filepath.Join(sourceDir, "Robyn", "Honey")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 0 {
		t.Errorf("expected 0 new, got %d", len(result.New))
	}
	if len(result.Valid) != 1 {
		t.Errorf("expected 1 valid, got %d", len(result.Valid))
	}
}

func TestDiff_Orphans(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Symlink in target pointing to a source that no longer exists
	mkdirs(t, targetDir, []string{"Robyn"})
	os.Symlink("/nonexistent/source", filepath.Join(targetDir, "Robyn", "Deleted"))

	// Empty scan result — nothing in source
	var scanned []string

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Orphan) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(result.Orphan))
	}
	if result.Orphan[0].TargetPath != filepath.Join(targetDir, "Robyn", "Deleted") {
		t.Errorf("unexpected orphan path: %s", result.Orphan[0].TargetPath)
	}
}

func TestDiff_ForeignEntry(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"real"})

	// Create a real directory in target where a symlink should be
	mkdirs(t, targetDir, []string{"foreign"})

	scanned := []string{filepath.Join(sourceDir, "real")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Foreign) != 1 {
		t.Errorf("expected 1 foreign entry, got %d", len(result.Foreign))
	}
	if len(result.New) != 1 {
		t.Errorf("expected 1 new entry, got %d", len(result.New))
	}
}

func TestDiff_MixedState(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"new-album", "existing-album"})

	// existing-album has a valid symlink
	os.Symlink(
		filepath.Join(sourceDir, "existing-album"),
		filepath.Join(targetDir, "existing-album"),
	)
	// orphan has a stale symlink
	os.Symlink("/gone", filepath.Join(targetDir, "orphan-album"))

	scanned := []string{
		filepath.Join(sourceDir, "new-album"),
		filepath.Join(sourceDir, "existing-album"),
	}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 1 {
		t.Errorf("expected 1 new, got %d", len(result.New))
	}
	if len(result.Valid) != 1 {
		t.Errorf("expected 1 valid, got %d", len(result.Valid))
	}
	if len(result.Orphan) != 1 {
		t.Errorf("expected 1 orphan, got %d", len(result.Orphan))
	}
}

func TestDiff_TargetDirNotExist(t *testing.T) {
	sourceDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"album"})
	targetDir := filepath.Join(t.TempDir(), "nonexistent")

	scanned := []string{filepath.Join(sourceDir, "album")}

	result, err := mirror.Diff(context.Background(), sourceDir, targetDir, scanned, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.New) != 1 {
		t.Errorf("expected 1 new, got %d", len(result.New))
	}
}

func sortEntries(entries []mirror.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TargetPath < entries[j].TargetPath
	})
}

func TestCreate_NewSymlink(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey"})

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "Robyn", "Honey"),
		TargetPath: filepath.Join(targetDir, "Robyn", "Honey"),
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Create(entry, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify symlink exists and points to correct source
	link, err := os.Readlink(entry.TargetPath)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if link != entry.SourcePath {
		t.Errorf("symlink points to %q, want %q", link, entry.SourcePath)
	}
}

func TestCreate_CreatesParentDirs(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"deep/nested/album"})

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "deep", "nested", "album"),
		TargetPath: filepath.Join(targetDir, "deep", "nested", "album"),
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Create(entry, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify parent dirs are real directories, not symlinks
	parentInfo, err := os.Lstat(filepath.Join(targetDir, "deep", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("parent directory should be a real dir, not a symlink")
	}
}

func TestCreate_DryRun(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	entry := mirror.Entry{
		SourcePath: filepath.Join(sourceDir, "album"),
		TargetPath: filepath.Join(targetDir, "album"),
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Create(entry, true, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Symlink should NOT exist in dry-run mode
	if _, err := os.Lstat(entry.TargetPath); !os.IsNotExist(err) {
		t.Error("symlink should not be created in dry-run mode")
	}
}

func TestPrune_RemovesSymlink(t *testing.T) {
	targetDir := t.TempDir()

	// Create a symlink to prune
	symlinkPath := filepath.Join(targetDir, "orphan")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Prune(entry, targetDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
		t.Error("symlink should have been removed")
	}
}

func TestPrune_CleansEmptyParents(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// Create nested structure: Artists/Robyn/Album (symlink)
	mkdirs(t, targetDir, []string{"Artists/Robyn"})
	symlinkPath := filepath.Join(prefixDir, "Robyn", "Album")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Robyn/ should be removed (now empty)
	if _, err := os.Lstat(filepath.Join(prefixDir, "Robyn")); !os.IsNotExist(err) {
		t.Error("empty parent 'Robyn' should have been removed")
	}

	// Artists/ (prefix dir) should NOT be removed
	if _, err := os.Lstat(prefixDir); os.IsNotExist(err) {
		t.Error("prefix directory should not be removed")
	}
}

func TestPrune_StopsAtPrefixBoundary(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// depth 1: symlink directly in prefix dir
	mkdirs(t, targetDir, []string{"Artists"})
	symlinkPath := filepath.Join(prefixDir, "Album")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prefix dir should still exist
	if _, err := os.Lstat(prefixDir); os.IsNotExist(err) {
		t.Error("prefix directory should not be removed")
	}
}

func TestPrune_KeepsNonEmptyParents(t *testing.T) {
	targetDir := t.TempDir()
	prefixDir := filepath.Join(targetDir, "Artists")

	// Artists/Robyn has two symlinks, we prune one
	mkdirs(t, targetDir, []string{"Artists/Robyn"})
	os.Symlink("/src/a", filepath.Join(prefixDir, "Robyn", "Album1"))
	os.Symlink("/src/b", filepath.Join(prefixDir, "Robyn", "Album2"))

	entry := mirror.Entry{TargetPath: filepath.Join(prefixDir, "Robyn", "Album1")}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Prune(entry, prefixDir, false, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Robyn/ should still exist (Album2 is still there)
	if _, err := os.Lstat(filepath.Join(prefixDir, "Robyn")); os.IsNotExist(err) {
		t.Error("non-empty parent should not be removed")
	}
}

func TestPrune_DryRun(t *testing.T) {
	targetDir := t.TempDir()
	symlinkPath := filepath.Join(targetDir, "orphan")
	os.Symlink("/some/source", symlinkPath)

	entry := mirror.Entry{TargetPath: symlinkPath}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := mirror.Prune(entry, targetDir, true, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Symlink should still exist in dry-run mode
	if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
		t.Error("symlink should not be removed in dry-run mode")
	}
}
