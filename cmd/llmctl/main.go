// Command llmctl is the entrypoint for the terminal dashboard and its
// non-interactive subcommands.
//
// Scaffold only: subcommand wiring lands with the vertical slice in Phase 2.
package main

import (
	"fmt"
	"os"
)

// version is set at build time by the release pipeline.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("llmctl", version)
		return
	}
	fmt.Fprintln(os.Stderr, "llmctl: not yet implemented — scaffold only")
	os.Exit(1)
}
