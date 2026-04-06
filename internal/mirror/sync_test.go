package mirror_test

import (
	"context"
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
