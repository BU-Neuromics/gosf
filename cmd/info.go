package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:          "info <project>",
	Short:        "Show metadata for an OSF project",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("info: not yet implemented")
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
