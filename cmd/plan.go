package cmd

import (
	"fmt"

	"github.com/argoproj/gitops-engine/pkg/utils/io"
	"github.com/argoproj/pkg/errors"
	"github.com/argoproj/pkg/kube/cli"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

func newPlanCmd() *cobra.Command {
	var (
		clientConfig clientcmd.ClientConfig
	)

	cmd := cobra.Command{
		Use: "plan",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSettings(clientConfig, false, true)
			errors.CheckError(err)

			engine, err := s.newEngine()
			errors.CheckError(err)

			closers, err := engine.Init()
			errors.CheckError(err)

			defer io.Close(closers)

			target, err := s.GetTarget()
			errors.CheckError(err)

			err = engine.Plan(target)
			if err != nil {
				fmt.Printf("failed to planning: %v", err)
			}
		},
	}
	clientConfig = cli.AddKubectlFlagsToCmd(&cmd)

	return &cmd
}

func init() {
	RootCmd.AddCommand(newPlanCmd())
}
