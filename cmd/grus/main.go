// Command grus is the Grus controller binary. Subcommands: controller, fetch, openapi, version.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: grus <controller|fetch|openapi|version> [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "controller":
		err = runController(os.Args[2:])
	case "fetch":
		err = runFetch(os.Args[2:])
	case "openapi":
		err = runOpenAPI()
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "grus %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
