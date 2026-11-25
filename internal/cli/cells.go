package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var cellColumns = []column{
	{"CELL", field("cell")},
	{"BOUNDS", func(o object) string {
		return fmt.Sprintf("x %s..%s, z %s..%s", field("bounds", "minX")(o), field("bounds", "maxX")(o),
			field("bounds", "minZ")(o), field("bounds", "maxZ")(o))
	}},
	{"INSTANCE", field("instance")}, {"ADDRESS", field("address")},
}

func newCellsCmd(env *Env, o *opts) *cobra.Command {
	return &cobra.Command{
		Use:   "cells <pool>",
		Short: "Show the region cells of a sharded pool and the instance that serves each (bounds in chunks)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := o.client(env)
			if err != nil {
				return err
			}
			cells, err := c.Cells(cmdContext(cmd), args[0])
			if err != nil {
				return err
			}
			return printList(cmd, o.output, cells, cellColumns)
		},
		ValidArgsFunction: completeFirstArg(completeResource(env, o, "pools")),
	}
}
