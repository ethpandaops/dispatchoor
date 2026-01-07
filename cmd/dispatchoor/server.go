package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethpandaops/dispatchoor/pkg/api"
	"github.com/ethpandaops/dispatchoor/pkg/auth"
	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/dispatcher"
	"github.com/ethpandaops/dispatchoor/pkg/github"
	"github.com/ethpandaops/dispatchoor/pkg/metrics"
	"github.com/ethpandaops/dispatchoor/pkg/queue"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newServerCmd(log *logrus.Logger) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the dispatchoor server",
		Long:  `Start the HTTP API server and dispatcher.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context(), log, configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml",
		"Path to configuration file")

	return cmd
}

func runServer(ctx context.Context, log *logrus.Logger, configPath string) error {
	// Load configuration.
	log.WithField("path", configPath).Info("Loading configuration")

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	log.Info("Configuration loaded:\n" + cfg.String())

	// Create store.
	var st store.Store

	switch cfg.Database.Driver {
	case "sqlite":
		st = store.NewSQLiteStore(log, cfg.Database.SQLite.Path)
	case "postgres":
		st = store.NewPostgresStore(log, cfg.GetDSN())
	default:
		log.Fatalf("Unsupported database driver: %s", cfg.Database.Driver)
	}

	// Start store.
	if err := st.Start(ctx); err != nil {
		return err
	}

	defer st.Stop()

	// Run migrations.
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	// Sync groups from config.
	if err := api.SyncGroupsFromConfig(ctx, log, st, cfg); err != nil {
		return err
	}

	// Create metrics.
	m := metrics.New()
	m.SetBuildInfo(Version, GitCommit, BuildDate)

	// Create GitHub client.
	ghClient := github.NewClient(log, cfg.GitHub.Token)

	if err := ghClient.Start(ctx); err != nil {
		return err
	}

	defer ghClient.Stop()

	// Create and start runner poller.
	poller := github.NewPoller(log, cfg, ghClient, st, m)

	if err := poller.Start(ctx); err != nil {
		return err
	}

	defer poller.Stop()

	// Create queue service.
	queueSvc := queue.NewService(log, st)

	if err := queueSvc.Start(ctx); err != nil {
		return err
	}

	defer queueSvc.Stop()

	// Create and start dispatcher.
	disp := dispatcher.NewDispatcher(log, cfg, st, queueSvc, ghClient)

	if err := disp.Start(ctx); err != nil {
		return err
	}

	defer disp.Stop()

	// Create and start auth service.
	authSvc := auth.NewService(log, cfg, st)

	if err := authSvc.Start(ctx); err != nil {
		return err
	}

	defer authSvc.Stop()

	// Create and start API server.
	srv := api.NewServer(log, cfg, st, queueSvc, authSvc, ghClient, m)

	// Set up runner change callbacks to broadcast via WebSocket.
	poller.SetRunnerChangeCallback(func(runner *store.Runner) {
		srv.BroadcastRunnerChange(runner)
	})

	disp.SetRunnerChangeCallback(func(runner *store.Runner) {
		srv.BroadcastRunnerChange(runner)
	})

	if err := srv.Start(ctx); err != nil {
		return err
	}

	defer srv.Stop()

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Info("Server is running. Press Ctrl+C to stop.")

	select {
	case sig := <-sigCh:
		log.WithField("signal", sig).Info("Received shutdown signal")
	case <-ctx.Done():
		log.Info("Context cancelled")
	}

	log.Info("Shutting down...")

	return nil
}
