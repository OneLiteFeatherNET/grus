// Command grus-agent will run instances on hosts outside Kubernetes (mc01). It is a stub in the
// prototype and must never import client-go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "grus-agent is not implemented yet")
	os.Exit(1)
}
