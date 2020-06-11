package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/argoproj/gitops-engine/pkg/utils/io"
	"github.com/argoproj/pkg/errors"
	"github.com/argoproj/pkg/kube/cli"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

func newApplyCmd() *cobra.Command {
	var (
		clientConfig clientcmd.ClientConfig
		prune        bool
		dryRun       bool
	)

	cmd := cobra.Command{
		Use: "apply",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSettings(clientConfig, prune, dryRun)
			errors.CheckError(err)

			engine, err := s.newEngine()
			errors.CheckError(err)

			closers, err := engine.Init()
			errors.CheckError(err)

			defer io.Close(closers)

			target, err := s.GetTarget()
			errors.CheckError(err)

			result, err := engine.Apply(
				context.Background(),
				target,
				s.ApplySyncSettings(),
			)
			if err != nil {
				fmt.Printf("failed to synchronize cluster state: %v", err)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "RESOURCE\tRESULT\n")
			for _, res := range result {
				fmt.Fprintf(w, "%s\t%s\n", res.ResourceKey.String(), res.Message)
			}
			w.Flush()
		},
	}
	clientConfig = cli.AddKubectlFlagsToCmd(&cmd)
	cmd.Flags().BoolVar(&prune, "prune", true, "Enables resource pruning.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Enables dryrun.")

	return &cmd
}

func init() {
	RootCmd.AddCommand(newApplyCmd())
}
