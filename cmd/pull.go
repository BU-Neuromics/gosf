package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:          "pull <project>[:<path>] [dest]",
	Short:        "Download files from an OSF project",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("pull: not yet implemented")
	},
}

func init() {
	pullCmd.Flags().Bool("dry-run", false, "Show what would be downloaded without downloading")
	pullCmd.Flags().BoolP("recursive", "r", false, "Download directories recursively")
	rootCmd.AddCommand(pullCmd)
}
