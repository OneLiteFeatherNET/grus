package cli

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/OneLiteFeatherNET/grus/internal/client"
)

// kind describes one resource type of the API as the CLI shows it.
type kind struct {
	name     string // plural, also the API path segment
	aliases  []string
	cols     []column
	newNames bool // false for resources every controller serves
}

var kinds = []kind{
	{name: "pools", aliases: []string{"pool", "po"}, cols: []column{
		{"NAME", field("name")}, {"ROLE", field("spec", "role")}, {"MODE", field("spec", "mode")},
		{"MIN", field("spec", "scaling", "min")}, {"MAX", field("spec", "scaling", "max")},
		{"GENERATION", field("generation")}, {"MAINTENANCE", field("maintenance")},
	}},
	{name: "instances", aliases: []string{"instance", "inst", "i"}, cols: []column{
		{"ID", field("id")}, {"POOL", field("pool")}, {"STATE", field("state")},
		{"PLAYERS", func(o object) string { return field("players")(o) + "/" + field("maxPlayers")(o) }},
		{"HOST", field("host")}, {"ADDRESS", field("address")},
	}},
	{name: "templates", aliases: []string{"template", "tpl"}, cols: []column{
		{"NAME", field("name")}, {"TAGS", templateTags},
	}},
	{name: "groups", aliases: []string{"group", "grp"}, newNames: true, cols: []column{
		{"NAME", field("name")}, {"TEMPLATES", field("spec", "templates")}, {"LABELS", field("spec", "labels")},
	}},
	{name: "environments", aliases: []string{"environment", "env"}, newNames: true, cols: []column{
		{"NAME", field("name")}, {"KIND", field("spec", "kind")}, {"IMAGE", field("spec", "image")},
		{"PORT", field("spec", "port")}, {"READINESS", field("spec", "readiness")},
	}},
	{name: "hosts", aliases: []string{"host"}, newNames: true, cols: []column{
		{"NAME", field("name")}, {"VERSION", field("version")}, {"ADDRESS", field("address")},
		{"PORTS", field("portRange")}, {"ONLINE", field("online")}, {"INSTANCES", field("instances")},
		{"LAST-SEEN", field("lastSeen")},
	}},
}

func kindNames() []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = k.name
	}
	return out
}

func lookupKind(name string) (kind, error) {
	n := strings.ToLower(name)
	for _, k := range kinds {
		if k.name == n {
			return k, nil
		}
		for _, a := range k.aliases {
			if a == n {
				return k, nil
			}
		}
	}
	return kind{}, fmt.Errorf("unknown resource %q, use one of: %s", name, strings.Join(kindNames(), ", "))
}

func templateTags(o object) string {
	tags, _ := o["tags"].(map[string]any)
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		d, _ := tags[k].(string)
		if len(d) > len("sha256:")+12 {
			d = d[:len("sha256:")+12]
		}
		parts = append(parts, k+"="+d)
	}
	return strings.Join(parts, " ")
}

// fetchAll lists a resource. Templates come as a map from the API and are turned into objects.
func fetchAll(cmd *cobra.Command, c *client.Client, k kind, q url.Values) ([]object, error) {
	if k.name != "templates" {
		return c.List(cmdContext(cmd), k.name, q)
	}
	all, err := c.Templates(cmdContext(cmd))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]object, 0, len(names))
	for _, n := range names {
		tags := make(map[string]any, len(all[n]))
		for t, d := range all[n] {
			tags[t] = d
		}
		out = append(out, object{"name": n, "tags": tags})
	}
	return out, nil
}

func fetchOne(cmd *cobra.Command, c *client.Client, k kind, name string) (object, error) {
	if k.name == "templates" {
		all, err := fetchAll(cmd, c, k, nil)
		if err != nil {
			return nil, err
		}
		for _, o := range all {
			if o["name"] == name {
				return o, nil
			}
		}
		return nil, fmt.Errorf("template %q not found", name)
	}
	return c.Get(cmdContext(cmd), k.name, name)
}

func newGetCmd(env *Env, o *opts) *cobra.Command {
	var pool, state, role string
	cmd := &cobra.Command{
		Use:   "get <pools|instances|templates|groups|environments|hosts> [name]",
		Short: "List resources or show one",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := lookupKind(args[0])
			if err != nil {
				return err
			}
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			if len(args) == 2 {
				obj, err := fetchOne(cmd, c, k, args[1])
				if err != nil {
					return err
				}
				if o.output == formatTable {
					return printList(cmd, o.output, []object{obj}, k.cols)
				}
				return printObject(cmd, o.output, obj)
			}
			q := url.Values{}
			if k.name == "instances" {
				for key, v := range map[string]string{"pool": pool, "state": state, "role": role} {
					if v != "" {
						q.Set(key, v)
					}
				}
			} else if pool != "" || state != "" || role != "" {
				return errors.New("--pool, --state and --role only apply to instances")
			}
			items, err := fetchAll(cmd, c, k, q)
			if err != nil {
				return err
			}
			return printList(cmd, o.output, items, k.cols)
		},
		ValidArgsFunction: completeKindAndName(env, o),
	}
	cmd.Flags().StringVar(&pool, "pool", "", "only instances of this pool")
	cmd.Flags().StringVar(&state, "state", "", "only instances in this state")
	cmd.Flags().StringVar(&role, "role", "", "only instances with this role (proxy, game, other)")
	_ = cmd.RegisterFlagCompletionFunc("pool", completeResource(env, o, "pools"))
	_ = cmd.RegisterFlagCompletionFunc("role", fixed("proxy", "game", "other"))
	return cmd
}

func newDescribeCmd(env *Env, o *opts) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <resource> <name>",
		Short: "Show one resource in detail; a pool also lists its instances",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := lookupKind(args[0])
			if err != nil {
				return err
			}
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			obj, err := fetchOne(cmd, c, k, args[1])
			if err != nil {
				return err
			}
			if err := printObject(cmd, o.output, obj); err != nil {
				return err
			}
			if k.name != "pools" || o.output != formatTable {
				return nil
			}
			insts, err := c.List(cmdContext(cmd), "instances", url.Values{"pool": {args[1]}})
			if err != nil {
				return err
			}
			if len(insts) == 0 {
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\ninstances:")
			ik, _ := lookupKind("instances")
			return writeTable(cmd.OutOrStdout(), insts, ik.cols)
		},
		ValidArgsFunction: completeKindAndName(env, o),
	}
}
