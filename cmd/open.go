package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:          "open <project>[:<path>]",
	Short:        "Open an OSF project or file in the browser",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("open: not yet implemented")
	},
}

func init() {
	rootCmd.AddCommand(openCmd)
}
