package mirror_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/adamrothman/shalott/internal/mirror"
)

// mkdirs creates directories under root from a list of relative paths.
func mkdirs(t *testing.T, root string, dirs []string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanSource_Depth1(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{
		"Soundtracks/TRON Legacy",
		"Soundtracks/Interstellar",
	})

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "Soundtracks")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_Depth2(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{
		"Robyn/Sexistential",
		"Robyn/Honey",
		"Charlie Puth/Whatever's Clever",
	})

	got, err := mirror.ScanSource(context.Background(), root, 2, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		filepath.Join(root, "Charlie Puth", "Whatever's Clever"),
		filepath.Join(root, "Robyn", "Honey"),
		filepath.Join(root, "Robyn", "Sexistential"),
	}
	assertPathsEqual(t, got, want)
}

func TestScanSource_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"real-dir"})

	// Create a symlink at depth 1
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "real-dir")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_SkipsSymlinksAtIntermediateDepth(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"real/album"})

	// Create a symlink at depth 1 that contains a depth-2 dir
	linkTarget := t.TempDir()
	mkdirs(t, linkTarget, []string{"album"})
	if err := os.Symlink(linkTarget, filepath.Join(root, "linked-artist")); err != nil {
		t.Fatal(err)
	}

	got, err := mirror.ScanSource(context.Background(), root, 2, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{filepath.Join(root, "real", "album")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_Debounce(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"old-dir", "new-dir"})

	now := time.Now()

	// Set old-dir mtime to 10 minutes ago
	oldTime := now.Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(root, "old-dir"), oldTime, oldTime)

	// new-dir mtime is ~now (just created)

	got, err := mirror.ScanSource(context.Background(), root, 1, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only old-dir should pass debounce (mtime > 5 min ago)
	want := []string{filepath.Join(root, "old-dir")}
	assertPathsEqual(t, got, want)
}

func TestScanSource_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, []string{"a", "b", "c", "d", "e"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := mirror.ScanSource(ctx, root, 1, 0, time.Now())
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestScanSource_EmptyDir(t *testing.T) {
	root := t.TempDir()

	got, err := mirror.ScanSource(context.Background(), root, 1, 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestScanSource_NonexistentDir(t *testing.T) {
	_, err := mirror.ScanSource(context.Background(), "/nonexistent", 1, 0, time.Now())
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func assertPathsEqual(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("expected %d paths, got %d:\n  want: %v\n  got:  %v", len(want), len(got), want, got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("path %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}
