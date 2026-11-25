package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/OneLiteFeatherNET/grus/internal/client"
)

const defaultTail = 200

func newScaleCmd(env *Env, o *opts) *cobra.Command {
	var minimum, maximum int
	cmd := &cobra.Command{
		Use:   "scale <pool> [--min N] [--max N]",
		Short: "Change the minimum (and maximum) instance count of a pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setMin, setMax := cmd.Flags().Changed("min"), cmd.Flags().Changed("max")
			if !setMin && !setMax {
				return errors.New("give --min and/or --max")
			}
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			pool, err := c.Get(cmdContext(cmd), "pools", args[0])
			if err != nil {
				return err
			}
			spec, _ := pool["spec"].(map[string]any)
			if spec == nil {
				return errors.New("the controller returned a pool without spec")
			}
			scaling, _ := spec["scaling"].(map[string]any)
			if scaling == nil {
				scaling = map[string]any{}
				spec["scaling"] = scaling
			}
			if setMin {
				scaling["min"] = minimum
			}
			if setMax {
				scaling["max"] = maximum
			}
			if _, err := c.Put(cmdContext(cmd), "pools", args[0], spec); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "pool/%s scaled (min=%s max=%s)\n", args[0],
				text(scaling["min"]), text(scaling["max"]))
			return err
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "pools")),
	}
	cmd.Flags().IntVar(&minimum, "min", 0, "minimum number of instances")
	cmd.Flags().IntVar(&maximum, "max", 0, "maximum number of instances")
	return cmd
}

func newStopCmd(env *Env, o *opts) *cobra.Command {
	var drain, force bool
	cmd := &cobra.Command{
		Use:   "stop <instance> [--drain|--force]",
		Short: "Stop an instance; one with players needs --drain or --force",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if drain && force {
				return errors.New("use either --drain or --force")
			}
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			inst, err := c.StopInstance(cmdContext(cmd), args[0], drain, force)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "instance/%s %s\n", args[0], text(inst["state"]))
			return err
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "instances")),
	}
	cmd.Flags().BoolVar(&drain, "drain", false, "wait for the players to leave before stopping")
	cmd.Flags().BoolVar(&force, "force", false, "stop now even with players on the instance")
	return cmd
}

func newLogsCmd(env *Env, o *opts) *cobra.Command {
	var follow bool
	var tail int
	cmd := &cobra.Command{
		Use:   "logs <instance> [-f] [--tail N]",
		Short: "Print the log of an instance, optionally following it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			if follow {
				return c.FollowLogs(cmdContext(cmd), args[0], func(line string) error {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), line)
					return err
				})
			}
			rc, err := c.Logs(cmdContext(cmd), args[0], tail)
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(cmd.OutOrStdout(), rc)
			return err
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "instances")),
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	cmd.Flags().IntVar(&tail, "tail", defaultTail, "number of trailing lines when not following")
	return cmd
}

func newExecCmd(env *Env, o *opts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <instance> -- <console line>",
		Short: "Send a console line to an instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			if err := c.Command(cmdContext(cmd), args[0], strings.Join(args[1:], " ")); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "sent to instance/%s\n", args[0])
			return err
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "instances")),
	}
	return cmd
}

func newEventsCmd(env *Env, o *opts) *cobra.Command {
	var prefix, lastID string
	cmd := &cobra.Command{
		Use:   "events [--type prefix]",
		Short: "Stream controller events until interrupted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			return c.Events(cmdContext(cmd), lastID, func(ev client.Event) error {
				if prefix != "" && !strings.HasPrefix(ev.Type, prefix) {
					return nil
				}
				if o.output == formatTable {
					_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", ev.ID, ev.Type, ev.Data)
					return err
				}
				return encodeEventLine(cmd.OutOrStdout(), o.output, ev)
			})
		},
	}
	cmd.Flags().StringVar(&prefix, "type", "", "only events whose type starts with this, e.g. instance.")
	cmd.Flags().StringVar(&lastID, "last-id", "", "resume after this event id")
	return cmd
}

func newNetworkCmd(env *Env, o *opts) *cobra.Command {
	return &cobra.Command{
		Use:   "network",
		Short: "Show the network-wide player count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			n, err := c.Network(cmdContext(cmd))
			if err != nil {
				return err
			}
			if o.output != formatTable {
				return printObject(cmd, o.output, n)
			}
			return printList(cmd, o.output, []object{n}, []column{
				{"PLAYERS", field("players")}, {"MAX", field("maxPlayers")}, {"PROXIES", field("proxies")},
			})
		},
	}
}

// encodeEventLine writes one event as a compact JSON line, or as a YAML document.
func encodeEventLine(w io.Writer, format string, ev client.Event) error {
	rec := map[string]any{"id": ev.ID, "type": ev.Type, "data": json.RawMessage(ev.Data)}
	if format == formatYAML {
		var data any
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			data = string(ev.Data)
		}
		rec["data"] = data
		if _, err := fmt.Fprintln(w, "---"); err != nil {
			return err
		}
		return encode(w, formatYAML, rec)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
