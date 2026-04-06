package mirror

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ScanSource walks sourceDir to exactly depth levels deep, returning absolute
// paths of real directories found at that depth. Skips symlinks and directories
// with mtime newer than now-debounce.
func ScanSource(ctx context.Context, sourceDir string, depth int, debounce time.Duration, now time.Time) ([]string, error) {
	var results []string
	err := walkToDepth(ctx, sourceDir, depth, func(path string, d fs.DirEntry) error {
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if debounce > 0 {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if now.Sub(info.ModTime()) < debounce {
				return nil
			}
		}
		results = append(results, path)
		return nil
	})
	return results, err
}

// walkToDepth walks dir to exactly depth levels, calling fn for each entry at
// that depth. At intermediate levels, only real directories are descended into
// (symlinks are never followed). Checks for context cancellation between entries.
func walkToDepth(ctx context.Context, dir string, depth int, fn func(path string, d fs.DirEntry) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		path := filepath.Join(dir, e.Name())

		if depth == 1 {
			if err := fn(path, e); err != nil {
				return err
			}
			continue
		}

		// Intermediate level: only descend into real directories
		if e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if !e.IsDir() {
			continue
		}

		if err := walkToDepth(ctx, path, depth-1, fn); err != nil {
			return err
		}
	}

	return nil
}
