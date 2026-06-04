package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:          "rm <project>:<path>",
	Short:        "Delete a file or folder from an OSF project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("rm: not yet implemented")
	},
}

func init() {
	rmCmd.Flags().Bool("dry-run", false, "Show what would be deleted without deleting")
	rootCmd.AddCommand(rmCmd)
}
