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

	// Demo mode until the storage, config and provider modules land.
	// Replacing these lines with the real services changes nothing inside
	// the app.
	deps := app.DemoDeps(900 * time.Millisecond)
	deps.Kinds = app.ProviderKinds(os.Getenv)

	p := tea.NewProgram(app.New(deps, app.DemoState()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "llmctl:", err)
		os.Exit(1)
	}
}
