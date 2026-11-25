// Command grus is the Grus controller and command line client.
// Server side subcommands: controller, fetch, openapi, version. The client commands live in internal/cli.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/OneLiteFeatherNET/grus/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	root := newRootCmd()
	cmd, err := root.ExecuteContextC(ctx)
	stop()
	if err != nil {
		name := "grus"
		if cmd != nil {
			name = cmd.CommandPath()
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// newRootCmd assembles the client commands and the server side subcommands.
func newRootCmd() *cobra.Command {
	root := cli.NewRoot(&cli.Env{})
	root.AddCommand(controllerCmd(), fetchCmd(), openapiCmd(), versionCmd())
	return root
}

// passthrough leaves flag parsing to the command's own flag set, so `controller` and `fetch` keep
// exactly their former flags and error messages.
func passthrough(use, short string, run func(args []string) error) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		RunE:               func(_ *cobra.Command, args []string) error { return run(args) },
	}
}

func controllerCmd() *cobra.Command {
	return passthrough("controller [flags]", "Run the controller", runController)
}

func fetchCmd() *cobra.Command {
	return passthrough("fetch --dest <dir>", "Download the templates named in GRUS_FETCH_TEMPLATES (init container)", runFetch)
}

func openapiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI document",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runOpenAPI(cmd.OutOrStdout()) },
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version) },
	}
}
