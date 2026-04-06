package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/adamrothman/shalott/internal/config"
	"github.com/adamrothman/shalott/internal/mirror"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "/etc/shalott/config.yaml", "path to config file")
	once := flag.Bool("once", false, "run a single scan and exit")
	dryRun := flag.Bool("dry-run", false, "log intended changes without executing")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("shalott", version)
		return 0
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return 2
	}

	// Create target directories
	for _, m := range cfg.Mirrors {
		if err := os.MkdirAll(m.Target, 0o755); err != nil {
			logger.Error("failed to create target directory",
				"target", m.Target,
				"error", err,
			)
			return 2
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

	if *once {
		return runOnce(cfg, *dryRun, logger)
	}
	return runDaemon(cfg, *dryRun, logger)
}

func runOnce(cfg *config.Config, dryRun bool, logger *slog.Logger) int {
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
		return 1
	}
	return 0
}

func runDaemon(cfg *config.Config, dryRun bool, logger *slog.Logger) int {
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
			return 0
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
