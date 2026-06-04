package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:          "push <src> <project>:<path>",
	Short:        "Upload files to an OSF project",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("push: not yet implemented")
	},
}

func init() {
	pushCmd.Flags().Bool("dry-run", false, "Show what would be uploaded without uploading")
	pushCmd.Flags().String("conflict", "skip", "Conflict resolution: skip, overwrite, or rename")
	rootCmd.AddCommand(pushCmd)
}
