package mirror

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/adamrothman/shalott/internal/config"
)

// ScanResult summarizes a single scan cycle.
type ScanResult struct {
	Created int
	Pruned  int
	Errors  int
}

// Mirror orchestrates scanning and syncing for a single mirror configuration.
type Mirror struct {
	Config config.Mirror
	DryRun bool
	Logger *slog.Logger
}

// RunOnce performs a single scan cycle across all path configs.
func (m *Mirror) RunOnce(ctx context.Context) ScanResult {
	var result ScanResult

	// Health check: verify source is reachable
	if _, err := os.Stat(m.Config.Source); err != nil {
		m.Logger.Error("source unreachable, skipping scan",
			"mirror", m.Config.Name,
			"source", m.Config.Source,
			"error", err,
		)
		result.Errors++
		return result
	}

	now := time.Now()

	for _, pathCfg := range m.Config.Paths {
		if ctx.Err() != nil {
			return result
		}

		sourceDir := filepath.Join(m.Config.Source, pathCfg.Prefix)
		targetDir := filepath.Join(m.Config.Target, pathCfg.Prefix)

		r := m.syncPath(ctx, sourceDir, targetDir, pathCfg, now)
		result.Created += r.Created
		result.Pruned += r.Pruned
		result.Errors += r.Errors
	}

	return result
}

func (m *Mirror) syncPath(ctx context.Context, sourceDir, targetDir string, pathCfg config.Path, now time.Time) ScanResult {
	var result ScanResult

	scanned, err := ScanSource(ctx, sourceDir, pathCfg.Depth, m.Config.Debounce.Duration, now)
	if err != nil {
		m.Logger.Error("scan failed, skipping path",
			"mirror", m.Config.Name,
			"prefix", pathCfg.Prefix,
			"error", err,
		)
		result.Errors++
		return result
	}

	m.Logger.Debug("scan complete",
		"mirror", m.Config.Name,
		"prefix", pathCfg.Prefix,
		"found", len(scanned),
	)

	diff, err := Diff(ctx, sourceDir, targetDir, scanned, pathCfg.Depth)
	if err != nil {
		m.Logger.Error("diff failed",
			"mirror", m.Config.Name,
			"prefix", pathCfg.Prefix,
			"error", err,
		)
		result.Errors++
		return result
	}

	for _, e := range diff.Foreign {
		m.Logger.Warn("foreign entry in target",
			"mirror", m.Config.Name,
			"path", e.TargetPath,
		)
	}

	for _, e := range diff.New {
		if err := Create(e, m.DryRun, m.Logger); err != nil {
			m.Logger.Error("failed to create symlink",
				"mirror", m.Config.Name,
				"target", e.TargetPath,
				"error", err,
			)
			result.Errors++
			continue
		}
		result.Created++
	}

	for _, e := range diff.Orphan {
		if err := Prune(e, targetDir, m.DryRun, m.Logger); err != nil {
			m.Logger.Error("failed to prune symlink",
				"mirror", m.Config.Name,
				"target", e.TargetPath,
				"error", err,
			)
			result.Errors++
			continue
		}
		result.Pruned++
	}

	return result
}

// Run starts the daemon loop, scanning on the configured interval.
// Runs an initial scan immediately, then on each ticker tick.
// Returns when ctx is cancelled. Send on forceScan to trigger an immediate scan.
func (m *Mirror) Run(ctx context.Context, forceScan <-chan struct{}) {
	m.Logger.Info("starting mirror",
		"mirror", m.Config.Name,
		"poll_interval", m.Config.PollInterval.Duration,
	)

	m.logScanResult("initial scan complete", m.RunOnce(ctx))

	ticker := time.NewTicker(m.Config.PollInterval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Logger.Info("stopping mirror", "mirror", m.Config.Name)
			return
		case <-ticker.C:
			m.logScanResult("scan complete", m.RunOnce(ctx))
		case <-forceScan:
			m.Logger.Info("forced scan triggered", "mirror", m.Config.Name)
			m.logScanResult("forced scan complete", m.RunOnce(ctx))
			ticker.Reset(m.Config.PollInterval.Duration)
		}
	}
}

func (m *Mirror) logScanResult(msg string, result ScanResult) {
	m.Logger.Debug(msg,
		"mirror", m.Config.Name,
		"created", result.Created,
		"pruned", result.Pruned,
		"errors", result.Errors,
	)
}
