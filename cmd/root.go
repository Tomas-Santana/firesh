package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tomas-santana/firesh/internal/repl"
)

var (
	projectID  string
	databaseID string
	outputFmt  string
)

var rootCmd = &cobra.Command{
	Use:   "firesh",
	Short: "A MongoDB-style CLI for Google Cloud Firestore",
	Long: `firesh — an interactive Firestore shell.

Uses Google Application Default Credentials (ADC) for authentication.
Run 'gcloud auth application-default login' or set GOOGLE_APPLICATION_CREDENTIALS.

Examples:
  firesh --project my-project
  firesh --project my-project --database my-db
  firesh --project my-project --output json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if projectID == "" {
			projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
			if projectID == "" {
				projectID = os.Getenv("GCLOUD_PROJECT")
			}
		}
		if projectID == "" {
			return fmt.Errorf("project ID is required: use --project or set GOOGLE_CLOUD_PROJECT")
		}
		if databaseID == "" {
			databaseID = "(default)"
		}
		r, err := repl.New(projectID, databaseID, outputFmt)
		if err != nil {
			return err
		}
		return r.Run()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&projectID, "project", "p", "", "GCP project ID (or set GOOGLE_CLOUD_PROJECT)")
	rootCmd.Flags().StringVarP(&databaseID, "database", "d", "", "Firestore database ID (default: \"(default)\")")
	rootCmd.Flags().StringVarP(&outputFmt, "output", "o", "table", "Output format: table, json, pretty")
}
