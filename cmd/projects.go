package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:          "projects",
	Short:        "List your OSF projects",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("projects: not yet implemented")
	},
}

func init() {
	rootCmd.AddCommand(projectsCmd)
}
