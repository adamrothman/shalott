package mirror

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Entry represents a source-target path pair for a mirrored directory.
type Entry struct {
	SourcePath string
	TargetPath string
}

// DiffResult classifies entries after comparing source scan to target state.
type DiffResult struct {
	New     []Entry // exist in source, no symlink in target
	Orphan  []Entry // symlink in target, not in source
	Valid   []Entry // symlink in target, exists in source
	Foreign []Entry // non-symlink entry in target
}

// Diff compares scanned source directories against existing entries in targetDir.
// Both sourceDir and targetDir should be at the prefix level.
func Diff(ctx context.Context, sourceDir, targetDir string, scanned []string, depth int) (*DiffResult, error) {
	sourceSet := make(map[string]string, len(scanned))
	for _, abs := range scanned {
		rel, err := filepath.Rel(sourceDir, abs)
		if err != nil {
			return nil, fmt.Errorf("computing relative path for %s: %w", abs, err)
		}
		sourceSet[rel] = abs
	}

	var result DiffResult

	// Check if target exists
	if _, err := os.Lstat(targetDir); os.IsNotExist(err) {
		for rel, sourcePath := range sourceSet {
			result.New = append(result.New, Entry{
				SourcePath: sourcePath,
				TargetPath: filepath.Join(targetDir, rel),
			})
		}
		return &result, nil
	}

	// Walk target at the same depth
	seen := make(map[string]bool)
	err := walkToDepth(ctx, targetDir, depth, func(path string, d fs.DirEntry) error {
		rel, err := filepath.Rel(targetDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, err)
		}
		seen[rel] = true

		isSymlink := d.Type()&fs.ModeSymlink != 0
		if !isSymlink {
			result.Foreign = append(result.Foreign, Entry{TargetPath: path})
			return nil
		}

		if sourcePath, inSource := sourceSet[rel]; inSource {
			result.Valid = append(result.Valid, Entry{SourcePath: sourcePath, TargetPath: path})
		} else {
			result.Orphan = append(result.Orphan, Entry{TargetPath: path})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking target: %w", err)
	}

	// Source entries not in target are new
	for rel, sourcePath := range sourceSet {
		if seen[rel] {
			continue
		}
		result.New = append(result.New, Entry{
			SourcePath: sourcePath,
			TargetPath: filepath.Join(targetDir, rel),
		})
	}

	return &result, nil
}

// Create creates a symlink at entry.TargetPath pointing to entry.SourcePath.
// Parent directories are created as needed.
func Create(entry Entry, dryRun bool, logger *slog.Logger) error {
	logger.Info("creating symlink",
		"target", entry.TargetPath,
		"source", entry.SourcePath,
		"dry_run", dryRun,
	)
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(entry.TargetPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directories: %w", err)
	}
	return os.Symlink(entry.SourcePath, entry.TargetPath)
}

// Prune removes the symlink at entry.TargetPath and cleans up empty parent
// directories up to (but not including) prefixDir.
func Prune(entry Entry, prefixDir string, dryRun bool, logger *slog.Logger) error {
	logger.Info("pruning symlink",
		"target", entry.TargetPath,
		"dry_run", dryRun,
	)
	if dryRun {
		return nil
	}
	if err := os.Remove(entry.TargetPath); err != nil {
		return fmt.Errorf("removing symlink: %w", err)
	}
	cleanupEmptyParents(entry.TargetPath, prefixDir)
	return nil
}

// cleanupEmptyParents removes empty directories between the parent of path
// and prefixDir (exclusive). Stops at the first non-empty directory.
func cleanupEmptyParents(path, prefixDir string) {
	prefixDir = filepath.Clean(prefixDir)
	dir := filepath.Dir(path)
	for {
		dir = filepath.Clean(dir)
		if dir == prefixDir || len(dir) <= len(prefixDir) {
			break
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}
