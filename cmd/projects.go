package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/output"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List your OSF projects",
	Long: `List all OSF projects and components accessible to the authenticated user.

Requires a valid token (set via 'gosf auth login', --token flag, or OSF_TOKEN).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := config.LoadToken(flagToken)
		if token == "" {
			return fmt.Errorf("projects requires authentication — run 'gosf auth login' or set OSF_TOKEN")
		}

		c := client.New(token)
		nodes, err := c.GetUserNodes(cmd.Context())
		if err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 401 {
				return fmt.Errorf("invalid token — run 'gosf auth login' to re-authenticate")
			}
			return err
		}

		if flagOutput == "json" {
			return output.PrintJSON(os.Stdout, nodes)
		}

		printNodesTable(nodes)
		return nil
	},
}

func printNodesTable(nodes []client.Node) {
	if len(nodes) == 0 {
		fmt.Fprintln(os.Stdout, "No projects found.")
		return
	}

	w := output.NewTabWriter(os.Stdout)
	defer w.Flush()

	fmt.Fprintln(w, "TITLE\tGUID\tVISIBILITY\tMODIFIED")
	for _, n := range nodes {
		vis := "private"
		if n.Attributes.Public {
			vis = "public"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			n.Attributes.Title,
			n.ID,
			vis,
			output.FormatDate(n.Attributes.DateModified),
		)
	}
}

func init() {
	rootCmd.AddCommand(projectsCmd)
}
