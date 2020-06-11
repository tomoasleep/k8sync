package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// RootCmd is a command without subcommand
var RootCmd = cobra.Command{
	Use: "k8sync",
}

func init() {
	log.SetLevel(log.FatalLevel)
}
