package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	// Build info (set via ldflags).
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"

	// Global flags.
	logLevel  string
	logFormat string
)

func main() {
	log := logrus.New()
	log.SetOutput(os.Stdout)

	rootCmd := &cobra.Command{
		Use:   "dispatchoor",
		Short: "GitHub Actions workflow orchestrator",
		Long: `dispatchoor releases jobs when runners are ready.

A workflow orchestrator for GitHub Actions that triggers jobs based on
runner availability, not blind schedules.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level, err := logrus.ParseLevel(logLevel)
			if err != nil {
				return err
			}
			log.SetLevel(level)

			switch logFormat {
			case "json":
				log.SetFormatter(&logrus.JSONFormatter{})
			default:
				log.SetFormatter(&logrus.TextFormatter{
					FullTimestamp: true,
				})
			}

			return nil
		},
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"Log level (trace, debug, info, warn, error, fatal)")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text",
		"Log format (text, json)")

	rootCmd.AddCommand(
		newServerCmd(log),
		newMigrateCmd(log),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
