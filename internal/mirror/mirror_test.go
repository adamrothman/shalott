package mirror_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adamrothman/shalott/internal/config"
	"github.com/adamrothman/shalott/internal/mirror"
)

func TestRunOnce_CreatesSymlinks(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"Robyn/Honey", "Robyn/Sexistential"})

	// Set mtime in the past to pass debounce
	past := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(sourceDir, "Robyn", "Honey"), past, past)
	os.Chtimes(filepath.Join(sourceDir, "Robyn", "Sexistential"), past, past)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:     "test",
			Source:   sourceDir,
			Target:   targetDir,
			Debounce: config.Duration{Duration: time.Minute},
			Paths: []config.Path{
				{Prefix: "Robyn", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Created != 2 {
		t.Errorf("expected 2 created, got %d", result.Created)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}

	// Verify symlinks exist
	for _, album := range []string{"Honey", "Sexistential"} {
		link := filepath.Join(targetDir, "Robyn", album)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("symlink %s not created: %v", link, err)
			continue
		}
		expected := filepath.Join(sourceDir, "Robyn", album)
		if target != expected {
			t.Errorf("symlink %s points to %q, want %q", link, target, expected)
		}
	}
}

func TestRunOnce_PrunesOrphans(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	// Create orphan symlink in target
	mkdirs(t, targetDir, []string{"prefix"})
	orphan := filepath.Join(targetDir, "prefix", "gone")
	os.Symlink("/nonexistent", orphan)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: sourceDir,
			Target: targetDir,
			Paths: []config.Path{
				{Prefix: "prefix", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Create the prefix dir in source so health check passes
	mkdirs(t, sourceDir, []string{"prefix"})

	result := m.RunOnce(context.Background())

	if result.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", result.Pruned)
	}

	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Error("orphan symlink should have been removed")
	}
}

func TestRunOnce_SourceUnreachable(t *testing.T) {
	targetDir := t.TempDir()

	// Create existing symlink that should NOT be pruned
	mkdirs(t, targetDir, []string{"prefix"})
	existing := filepath.Join(targetDir, "prefix", "keep")
	os.Symlink("/whatever", existing)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:   "test",
			Source: "/nonexistent/source",
			Target: targetDir,
			Paths: []config.Path{
				{Prefix: "prefix", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}

	// Existing symlink should be preserved (no pruning when source is down)
	if _, err := os.Lstat(existing); os.IsNotExist(err) {
		t.Error("existing symlink should not be pruned when source is unreachable")
	}
}

func TestRunOnce_DryRun(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	mkdirs(t, sourceDir, []string{"album"})

	past := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(sourceDir, "album"), past, past)

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:     "test",
			Source:   sourceDir,
			Target:   targetDir,
			Debounce: config.Duration{Duration: time.Minute},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		DryRun: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result := m.RunOnce(context.Background())

	if result.Created != 1 {
		t.Errorf("expected 1 created (dry run), got %d", result.Created)
	}

	// Symlink should NOT actually exist
	if _, err := os.Lstat(filepath.Join(targetDir, "album")); !os.IsNotExist(err) {
		t.Error("symlink should not be created in dry-run mode")
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:         "test",
			Source:       sourceDir,
			Target:       targetDir,
			PollInterval: config.Duration{Duration: time.Hour},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx, nil)
		close(done)
	}()

	// Give Run time to start and do initial scan
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestRun_ForceScan(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	m := &mirror.Mirror{
		Config: config.Mirror{
			Name:         "test",
			Source:       sourceDir,
			Target:       targetDir,
			PollInterval: config.Duration{Duration: time.Hour},
			Paths: []config.Path{
				{Prefix: "", Depth: 1},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forceScan := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		m.Run(ctx, forceScan)
		close(done)
	}()

	// Wait for initial scan to complete
	time.Sleep(50 * time.Millisecond)

	// Add a new directory and force scan
	mkdirs(t, sourceDir, []string{"new-album"})
	forceScan <- struct{}{}

	// Wait for forced scan to complete
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Verify the new symlink was created
	if _, err := os.Lstat(filepath.Join(targetDir, "new-album")); os.IsNotExist(err) {
		t.Error("forced scan should have created the new symlink")
	}
}
