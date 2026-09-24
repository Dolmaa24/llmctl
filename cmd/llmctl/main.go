// Command llmctl is the entrypoint for the terminal dashboard and its
// non-interactive subcommands.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Dolmaa24/llmctl/internal/app"
	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time by the release pipeline.
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println("llmctl", version)
			return
		default:
			fmt.Fprintf(os.Stderr, "llmctl: unknown command %q\n", os.Args[1])
			os.Exit(2)
		}
	}

	// Demo mode until the storage repositories and provider adapters land.
	// Swapping them in replaces these two lines and nothing inside the app.
	providers, messages, notes, checks := app.DemoState()
	registry := app.DemoRegistry(900 * time.Millisecond)

	p := tea.NewProgram(
		app.New(registry, providers, messages, notes, checks),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "llmctl:", err)
		os.Exit(1)
	}
}
