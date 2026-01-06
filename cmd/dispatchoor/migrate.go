package main

import (
	"context"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newMigrateCmd(log *logrus.Logger) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		Long:  `Run database migrations to create or update the schema.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd.Context(), log, configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml",
		"Path to configuration file")

	return cmd
}

func runMigrate(ctx context.Context, log *logrus.Logger, configPath string) error {
	// Load configuration.
	log.WithField("path", configPath).Info("Loading configuration")

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

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

	log.Info("Migrations completed successfully")

	return nil
}
