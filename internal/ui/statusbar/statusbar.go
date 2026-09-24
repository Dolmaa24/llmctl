// Package statusbar renders the continuous diagnostics strip along the bottom.
package statusbar

import (
	"fmt"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Check is one result to display. Like providerpane.Item this is a view-model
// rather than doctor.CheckResult, so the ui tree does not import the
// Windows/WSL-specific doctor package just to draw a coloured dot.
type Check struct {
	Name    string
	Status  string // "ok" | "warn" | "fail"
	Message string
}

type Model struct {
	checks  []Check
	updated time.Time
	width   int
}

func New(checks []Check) Model {
	return Model{checks: checks, updated: time.Now()}
}

func (m *Model) SetChecks(checks []Check) {
	m.checks = checks
	m.updated = time.Now()
}

// SetCheck replaces the result of an existing check. A check the bar does not
// know about is ignored: which checks exist is the diagnostics module's
// decision, not the caller's.
func (m *Model) SetCheck(name, status, message string) {
	for i := range m.checks {
		if m.checks[i].Name == name {
			m.checks[i].Status, m.checks[i].Message = status, message
			return
		}
	}
}

func (m *Model) SetWidth(w int) { m.width = w }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(tea.Msg) (Model, tea.Cmd) { return m, nil }

func (m Model) View() string {
	if len(m.checks) == 0 {
		return lipgloss.NewStyle().Width(m.width).Render(styles.Ghost.Render(" no checks registered"))
	}

	var parts []string
	for _, c := range m.checks {
		dot := lipgloss.NewStyle().Foreground(styles.StatusColour(c.Status)).Render(symbol(c.Status))
		label := styles.Dim.Render(c.Name)
		// A failing check earns its detail text; a passing one would just be
		// noise across a narrow bar.
		if c.Status != "ok" && c.Message != "" {
			label += styles.Ghost.Render(" " + c.Message)
		}
		parts = append(parts, dot+" "+label)
	}

	left := " " + strings.Join(parts, styles.Ghost.Render("  │  "))
	right := styles.Ghost.Render(fmt.Sprintf("%s ", m.updated.Format("15:04:05")))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Too narrow for the clock: drop it rather than wrapping the bar onto
		// a second line and shifting the layout.
		return lipgloss.NewStyle().Width(m.width).MaxHeight(1).Render(left)
	}
	return left + strings.Repeat(" ", gap) + right
}

func symbol(status string) string {
	switch status {
	case "ok":
		return "●"
	case "warn":
		return "◐"
	case "fail":
		return "○"
	default:
		return "·"
	}
}
