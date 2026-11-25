package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var privateColumns = []column{
	{"ID", field("id")}, {"POOL", field("pool")}, {"OWNER", field("labels", "grus.olf/owner")},
	{"STATE", field("state")}, {"PLAYERS", field("players")}, {"ADDRESS", field("address")},
}

func newPrivateCmd(env *Env, o *opts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "private",
		Short: "Start and list private servers (instances owned by one player)",
	}
	cmd.AddCommand(newPrivateCreateCmd(env, o), newPrivateListCmd(env, o))
	return cmd
}

func newPrivateCreateCmd(env *Env, o *opts) *cobra.Command {
	var owner string
	cmd := &cobra.Command{
		Use:   "create <pool> --owner <owner>",
		Short: "Start the private server of an owner, or show the one that already runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" {
				return errors.New("give --owner")
			}
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			inst, created, err := c.CreatePrivateServer(cmdContext(cmd), args[0], owner)
			if err != nil {
				return err
			}
			if o.output != formatTable {
				return printObject(cmd, o.output, inst)
			}
			verb := "already running"
			if created {
				verb = "started"
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "instance/%s %s (%s)\n", text(inst["id"]), verb, text(inst["state"]))
			return err
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "pools")),
	}
	cmd.Flags().StringVar(&owner, "owner", "", "owner of the server, e.g. a player uuid")
	return cmd
}

func newPrivateListCmd(env *Env, o *opts) *cobra.Command {
	var owner string
	cmd := &cobra.Command{
		Use:   "list [--owner <owner>]",
		Short: "List private servers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			list, err := c.PrivateServers(cmdContext(cmd), owner)
			if err != nil {
				return err
			}
			return printList(cmd, o.output, list, privateColumns)
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "only the servers of this owner")
	return cmd
}
