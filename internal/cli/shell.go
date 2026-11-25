package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/reeflective/console"
	"github.com/spf13/cobra"
)

const historyFileName = "history"

func newExitCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:     "exit",
		Aliases: []string{"quit"},
		Short:   "Leave the shell",
		Args:    cobra.NoArgs,
		Run:     func(*cobra.Command, []string) { env.Exit(0) },
	}
}

// newShellCmd is `grus shell`: a REPL over the same command tree. The tree is rebuilt for every line,
// seeded with the flags given to `grus shell`, so a flag of one line never leaks into the next.
func newShellCmd(env *Env, o *opts) *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Start an interactive shell with history and live completion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := env.resolve(o)
			if err != nil {
				return err
			}
			seed := *o
			app := console.New("grus")
			menu := app.ActiveMenu()
			menu.Prompt().Primary = func() string { return promptFor(conn) }
			menu.SetCommands(func() *cobra.Command { return newRoot(env, seed, true) })
			menu.AddInterrupt(io.EOF, func(*console.Console) { env.Exit(0) })
			if dir, err := env.configDir(); err == nil && os.MkdirAll(dir, 0o700) == nil {
				menu.AddHistorySourceFile("grus", filepath.Join(dir, historyFileName))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "grus shell connected to %s, type exit or press Ctrl-D to leave\n", conn.URL)
			return app.StartContext(cmdContext(cmd))
		},
	}
}

func promptFor(conn connection) string {
	name := conn.Context
	if name == "" {
		name = "-"
	}
	return "grus(" + name + ")> "
}
