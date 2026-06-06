// Package cli wires up the command-line interface using Cobra.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the root "toy-build-oci" command and attaches subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "toy-build-oci",
		Short: "A minimal OCI image builder",
		Long: "toy-build-oci builds an OCI image (a docker-archive tar loadable by\n" +
			"docker/podman) from a rootfs directory. It exists to show how Docker and\n" +
			"BuildKit assemble images, step by step.",
		SilenceUsage:  true, // don't dump usage on runtime errors
		SilenceErrors: true, // main() prints the error itself
	}
	root.AddCommand(newBuildCmd())
	return root
}

// Execute runs the root command.
func Execute() error {
	return NewRootCmd().Execute()
}
