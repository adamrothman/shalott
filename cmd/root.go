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
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := newLogger()

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		for _, m := range cfg.Mirrors {
			if _, err := os.Stat(m.Source); err != nil {
				logger.Warn("source not reachable at startup",
					"mirror", m.Name,
					"source", m.Source,
					"error", err,
				)
			}
			if err := os.MkdirAll(m.Target, 0o755); err != nil {
				return fmt.Errorf("creating target directory %s: %w", m.Target, err)
			}
		}

		ctx := cmd.Context()
		if once {
			return runOnce(ctx, cfg, dryRun, logger)
		}
		return runDaemon(ctx, cfg, dryRun, logger)
	},
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
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if debug {
		opts.Level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func runOnce(ctx context.Context, cfg *config.Config, dryRun bool, logger *slog.Logger) error {
	hasErrors := false

	for _, mcfg := range cfg.Mirrors {
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		result := m.RunOnce(ctx)
		if len(result.Errors) > 0 {
			hasErrors = true
		}
	}

	if hasErrors {
		return errors.New("one or more mirrors encountered errors")
	}
	return nil
}

func runDaemon(ctx context.Context, cfg *config.Config, dryRun bool, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)

	var wg sync.WaitGroup
	forceScan := mirror.NewBroadcast()

	for _, mcfg := range cfg.Mirrors {
		m := &mirror.Mirror{
			Config: mcfg,
			DryRun: dryRun,
			Logger: logger,
		}
		wg.Go(func() {
			m.Run(ctx, forceScan)
		})
	}

	logger.Info("shalott started", "mirrors", len(cfg.Mirrors))

	for {
		select {
		case sig := <-stopCh:
			logger.Info("received signal, shutting down", "signal", sig)
			cancel()
			wg.Wait()
			logger.Info("shalott stopped")
			return nil
		case <-reloadCh:
			logger.Info("received SIGHUP, forcing scan on all mirrors")
			forceScan.Notify()
		}
	}
}
