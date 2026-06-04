package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var lsCmd = &cobra.Command{
	Use:   "ls <project>[:<path>]",
	Short: "List files in an OSF project or folder",
	Long: `List files and folders at the given OSF path.

Examples:
  gosf ls abc12                  # list root of project abc12
  gosf ls abc12:/data            # list the /data folder
  gosf ls abc12/xyz34:/results   # list /results in component xyz34`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := resolver.ParseTarget(args[0])
		if err != nil {
			return err
		}

		token := config.LoadToken(flagToken)
		c := client.New(token)
		res := resolver.New(c)

		items, err := res.ListDir(context.Background(), target.NodeID, target.Path)
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			return output.PrintJSON(os.Stdout, items)
		}

		printFileTable(items)
		return nil
	},
}

func printFileTable(items []client.FileItem) {
	w := output.NewTabWriter(os.Stdout)
	defer w.Flush()

	output.PrintHeader(w)
	for _, item := range items {
		name := item.Attributes.Name
		size := "—"
		if item.Attributes.Kind == "folder" {
			name = name + "/"
		} else {
			size = output.FormatSize(item.Attributes.Size)
		}
		modified := output.FormatDate(item.Attributes.DateModified)
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, size, modified)
	}
}

func init() {
	rootCmd.AddCommand(lsCmd)
}
