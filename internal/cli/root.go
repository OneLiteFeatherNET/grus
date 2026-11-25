package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/OneLiteFeatherNET/grus/internal/client"
)

// opts holds the persistent flags every client command shares.
type opts struct {
	context   string
	url       string
	tokenFile string
	output    string
}

// NewRoot returns the root `grus` command with all client commands. The caller adds the server side
// commands (controller, fetch, ...) when it wants them.
func NewRoot(env *Env) *cobra.Command {
	env.ensure()
	return newRoot(env, opts{output: formatTable}, false)
}

// newRoot builds a fresh tree; the shell calls it before every line so no flag state leaks between lines.
// defaults seed the persistent flags (the shell inherits the flags of `grus shell`).
func newRoot(env *Env, defaults opts, inShell bool) *cobra.Command {
	o := defaults
	root := &cobra.Command{
		Use:           "grus",
		Short:         "Grus game server orchestration",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !validFormat(o.output) {
				return fmt.Errorf("invalid output format %q, use table, json or yaml", o.output)
			}
			return nil
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&o.context, "context", o.context, "context from the config file to use")
	pf.StringVar(&o.url, "url", o.url, "controller URL (overrides the context) [GRUS_URL]")
	pf.StringVar(&o.tokenFile, "token-file", o.tokenFile, "file with the API token (token env: GRUS_TOKEN)")
	pf.StringVarP(&o.output, "output", "o", o.output, "output format: table, json or yaml")
	_ = root.RegisterFlagCompletionFunc("context", completeContexts(env))
	_ = root.RegisterFlagCompletionFunc("output", fixed(formatTable, formatJSON, formatYAML))

	root.AddCommand(
		newGetCmd(env, &o), newDescribeCmd(env, &o), newApplyCmd(env, &o), newScaleCmd(env, &o),
		newStopCmd(env, &o), newLogsCmd(env, &o), newExecCmd(env, &o), newEventsCmd(env, &o),
		newNetworkCmd(env, &o), newPrivateCmd(env, &o), newLoginCmd(env), newConfigCmd(env),
	)
	if inShell {
		root.AddCommand(newExitCmd(env))
	} else {
		root.AddCommand(newShellCmd(env, &o))
	}
	return root
}

// client builds an API client for the resolved connection.
func (o *opts) client(env *Env) (*client.Client, connection, error) {
	conn, err := env.resolve(o)
	if err != nil {
		return nil, connection{}, err
	}
	return client.New(conn.URL, conn.Token, env.HTTPClient), conn, nil
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
