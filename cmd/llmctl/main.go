// Command llmctl is the entrypoint for the terminal dashboard and its
// non-interactive subcommands.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Dolmaa24/llmctl/internal/doctor"
)

// version is set at build time by the release pipeline.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "version":
		fmt.Println("llmctl", version)
		return
	case "doctor":
		exitCode := runDoctor(context.Background())
		os.Exit(exitCode)
	default:
		printUsage()
		os.Exit(2)
	}
}

func runDoctor(ctx context.Context) int {
	runner := doctor.NewRunner()
	runner.Register(doctor.WSLCheck{})
	runner.Register(doctor.NetworkCheck{})
	runner.Register(doctor.RedisCheck{})
	runner.Register(doctor.AuthCheck{Provider: "provider"})

	results := runner.RunAll(ctx)
	hasFail := false
	for _, r := range results {
		fmt.Printf("[%s] %-14s %s (%s)\n", r.Status, r.Name, r.Message, r.CheckedAt.Format(time.RFC3339))
		if r.Status == doctor.StatusFail {
			hasFail = true
		}
	}

	if hasFail {
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: llmctl [doctor|version]")
}
