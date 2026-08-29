package cli

import (
	"github.com/spf13/cobra"
)

// version is overwritten at build time via -ldflags.
var version = "dev"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "platform",
		Short: "Scaffold services onto the internal developer platform",
		Long: `platform scaffolds the files a new service needs to run on the IDP.

It generates the ArgoCD values file that onboards the service, plus the CI
workflow and README for the service repository. It deliberately does not
generate an ArgoCD Application: the platform's ApplicationSet creates one
automatically from the values file.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.AddCommand(newInitCommand())
	return root
}
