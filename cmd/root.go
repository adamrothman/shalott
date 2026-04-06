package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/adamrothman/shalott/internal/config"
	"github.com/adamrothman/shalott/internal/mirror"
)

var version = "dev"

var (
	configPath string
	once       bool
	dryRun     bool
	debug      bool
)

var rootCmd = &cobra.Command{
	Use:          "shalott",
	Short:        "Symlink mirror daemon",
	Long:         "A polling daemon that maintains a local symlink tree mirroring a remote source, generating inotify events for watching applications.",
	Version:      version,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runRoot,
}

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "/etc/shalott/config.yaml", "path to config file")
	rootCmd.Flags().BoolVar(&once, "once", false, "run a single scan and exit")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "log intended changes without executing")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging")
}

// Execute runs the root command and returns any error.
func Execute() error {
	return rootCmd.Execute()
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func runRoot(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Create target directories
	for _, m := range cfg.Mirrors {
		if err := os.MkdirAll(m.Target, 0o755); err != nil {
			return fmt.Errorf("creating target directory %s: %w", m.Target, err)
		}
	}

	// Warn about unreachable sources
	for _, m := range cfg.Mirrors {
		if _, err := os.Stat(m.Source); err != nil {
			logger.Warn("source not reachable at startup",
				"mirror", m.Name,
				"source", m.Source,
				"error", err,
			)
		}
	}

	if once {
		return runOnce(cfg, dryRun, logger)
	}
	return runDaemon(cfg, dryRun, logger)
}

func runOnce(cfg *config.Config, dryRun bool, logger *slog.Logger) error {
	ctx := context.Background()
	hasErrors := false

	for _, mcfg := range cfg.Mirrors {
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		result := m.RunOnce(ctx)
		if result.Errors > 0 {
			hasErrors = true
		}
	}

	if hasErrors {
		return errors.New("one or more mirrors encountered errors")
	}
	return nil
}

func runDaemon(cfg *config.Config, dryRun bool, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling: SIGTERM/SIGINT for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Signal handling: SIGHUP for forced scan
	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)

	// Start mirror goroutines
	var wg sync.WaitGroup
	forceScanChs := make([]chan struct{}, len(cfg.Mirrors))

	for i, mcfg := range cfg.Mirrors {
		forceScanChs[i] = make(chan struct{}, 1)
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		wg.Add(1)
		go func(forceScan chan struct{}) {
			defer wg.Done()
			m.Run(ctx, forceScan)
		}(forceScanChs[i])
	}

	logger.Info("shalott started", "mirrors", len(cfg.Mirrors))

	for {
		select {
		case sig := <-sigCh:
			logger.Info("received signal, shutting down", "signal", sig)
			cancel()
			wg.Wait()
			logger.Info("shalott stopped")
			return nil
		case <-sighupCh:
			logger.Info("received SIGHUP, forcing scan on all mirrors")
			for _, ch := range forceScanChs {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}
}
