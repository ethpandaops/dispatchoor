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

	// Create GitHub clients.
	// - runnersClient: used for polling runner status (uses runners_token if set, else token)
	// - dispatchClient: used for dispatching workflows (uses token)
	var runnersClient github.Client

	var dispatchClient github.Client

	var poller github.Poller

	// Create runners client for polling (uses runners_token if configured, else falls back to token).
	if cfg.HasRunnersToken() {
		runnersToken := cfg.GetRunnersToken()
		runnersClient = github.NewClient(log.WithField("client", "runners"), runnersToken)

		if err := runnersClient.Start(ctx); err != nil {
			return err
		}

		defer func() {
			if err := runnersClient.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop runners GitHub client")
			}
		}()

		// Only start poller if runners client is connected.
		if runnersClient.IsConnected() {
			poller = github.NewPoller(log, cfg, runnersClient, st, m)

			if err := poller.Start(ctx); err != nil {
				return err
			}

			defer func() {
				if err := poller.Stop(); err != nil {
					log.WithError(err).Warn("Failed to stop poller")
				}
			}()
		} else {
			log.Warn("Runners GitHub client not connected - runner polling disabled")
		}
	} else {
		log.Warn("No GitHub token configured for runners - runner polling disabled")
	}

	// Create dispatch client for workflow dispatching (uses main token).
	if cfg.HasGitHubToken() {
		dispatchClient = github.NewClient(log.WithField("client", "dispatch"), cfg.GitHub.Token)

		if err := dispatchClient.Start(ctx); err != nil {
			return err
		}

		defer func() {
			if err := dispatchClient.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop dispatch GitHub client")
			}
		}()

		if !dispatchClient.IsConnected() {
			log.Warn("Dispatch GitHub client not connected - workflow dispatch disabled")
		}
	} else {
		log.Warn("No GitHub token configured for dispatch - workflow dispatch disabled")
	}

	// Create queue service.
	queueSvc := queue.NewService(log, cfg, st)

	if err := queueSvc.Start(ctx); err != nil {
		return err
	}

	defer queueSvc.Stop()

	// Create and start dispatcher (only if dispatch client is connected).
	var disp dispatcher.Dispatcher

	if dispatchClient != nil && dispatchClient.IsConnected() {
		disp = dispatcher.NewDispatcher(log, cfg, st, queueSvc, dispatchClient)

		if err := disp.Start(ctx); err != nil {
			return err
		}

		defer func() {
			if err := disp.Stop(); err != nil {
				log.WithError(err).Warn("Failed to stop dispatcher")
			}
		}()
	}

	// Create and start auth service.
	authSvc := auth.NewService(log, cfg, st)

	if err := authSvc.Start(ctx); err != nil {
		return err
	}

	defer authSvc.Stop()

	// Create and start API server.
	srv := api.NewServer(log, cfg, st, queueSvc, authSvc, dispatchClient, m)

	// Set up runner change callbacks to broadcast via WebSocket.
	if poller != nil {
		poller.SetRunnerChangeCallback(func(runner *store.Runner) {
			srv.BroadcastRunnerChange(runner)
		})
	}

	if disp != nil {
		disp.SetRunnerChangeCallback(func(runner *store.Runner) {
			srv.BroadcastRunnerChange(runner)
		})
	}

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
