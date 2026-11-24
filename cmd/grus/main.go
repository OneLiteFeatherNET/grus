// Command grus is the Grus controller binary. Subcommands: controller, fetch.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: grus <controller|fetch> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}
