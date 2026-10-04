// Command llmctl is the entrypoint for the terminal dashboard and its
// non-interactive subcommands.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Dolmaa24/llmctl/internal/app"
	"github.com/Dolmaa24/llmctl/internal/doctor"
	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time by the release pipeline.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(runTUI())
	}

	switch args[0] {
	case "version":
		fmt.Println("llmctl", version)
		return
	case "doctor":
		exitCode := runDoctor(context.Background())
		os.Exit(exitCode)
	default:
		fmt.Fprintf(os.Stderr, "llmctl: unknown command %q\n", args[0])
		printUsage()
		os.Exit(2)
	}
}

// runTUI starts the terminal dashboard and returns the exit code.
func runTUI() int {
	// Demo mode until the storage, config and provider modules land.
	// Replacing these lines with the real services changes nothing inside
	// the app.
	deps := app.DemoDeps(900 * time.Millisecond)
	deps.Kinds = app.ProviderKinds(os.Getenv)

	p := tea.NewProgram(app.New(deps, app.DemoState()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "llmctl:", err)
		return 1
	}
	return 0
}

func runDoctor(ctx context.Context) int {
	runner := doctor.NewRunner()
	runner.Register(doctor.WSLCheck{})
	runner.Register(doctor.NetworkCheck{})
	runner.Register(doctor.OllamaCheck{})
	runner.Register(doctor.GPUCheck{})
	runner.Register(doctor.AuthCheck{Provider: "ollama"})

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
	fmt.Fprintln(os.Stderr, "Usage: llmctl [doctor|version]  (no command opens the dashboard)")
}
