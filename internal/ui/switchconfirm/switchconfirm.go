// Package switchconfirm renders the pre-switch cost comparison.
//
// This is the most demo-critical piece of the interface: it is where the
// project's central claim becomes visible to the user, so it has to make the
// comparison legible at a glance rather than merely printing two numbers.
package switchconfirm

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Plan is what the modal displays. It is a view-model of session.HandoffPlan
// plus the target of the switch.
//
// Deliberately no currency field: prices change faster than this project
// ships, and a stale rate would put a confidently wrong number on the one
// screen the whole project is judged by. Tokens are the stable unit.
type Plan struct {
	FromProvider, FromModel string
	ToProvider, ToModel     string

	NotesCount   int
	RawTurnCount int

	TokensFullReplay int
	TokensDistilled  int

	// TokensExtraction is what distillation has already spent extracting notes
	// this session. Showing it keeps the comparison honest — the saving is not
	// free, and a reviewer will ask.
	TokensExtraction int
}

// Saving returns the reduction against full replay, as a percentage.
func (p Plan) Saving() float64 {
	if p.TokensFullReplay == 0 {
		return 0
	}
	d := float64(p.TokensFullReplay-p.TokensDistilled) / float64(p.TokensFullReplay)
	return d * 100
}

// NetSaving accounts for extraction already spent this session.
func (p Plan) NetSaving() float64 {
	if p.TokensFullReplay == 0 {
		return 0
	}
	total := p.TokensDistilled + p.TokensExtraction
	return float64(p.TokensFullReplay-total) / float64(p.TokensFullReplay) * 100
}

// ConfirmedMsg and CancelledMsg report the user's decision. The modal performs
// no side effects; the root model executes or abandons the switch.
type (
	ConfirmedMsg struct{}
	CancelledMsg struct{}
)

type keyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

var keys = keyMap{
	Confirm: key.NewBinding(key.WithKeys("enter", "y")),
	Cancel:  key.NewBinding(key.WithKeys("esc", "n", "q")),
}

type Model struct {
	plan    Plan
	visible bool
	width   int
	height  int
}

func New() Model { return Model{} }

func (m *Model) Show(p Plan)  { m.plan, m.visible = p, true }
func (m *Model) Hide()        { m.visible = false }
func (m Model) Visible() bool { return m.visible }

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, keys.Confirm):
			m.visible = false
			return m, func() tea.Msg { return ConfirmedMsg{} }
		case key.Matches(km, keys.Cancel):
			m.visible = false
			return m, func() tea.Msg { return CancelledMsg{} }
		}
	}
	return m, nil
}

const barWidth = 34

// bar draws a proportional bar so the two options can be compared by length,
// not by reading digits. Colour alone is never the only cue.
func bar(value, max int, colour lipgloss.TerminalColor) string {
	if max <= 0 {
		return ""
	}
	filled := value * barWidth / max
	if filled < 1 && value > 0 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}
	return lipgloss.NewStyle().Foreground(colour).Render(strings.Repeat("█", filled)) +
		styles.Ghost.Render(strings.Repeat("░", barWidth-filled))
}

func (m Model) View() string {
	if !m.visible {
		return ""
	}
	p := m.plan

	head := styles.Title.Render("Switch provider") + "\n" +
		styles.Dim.Render(fmt.Sprintf("%s: %s  →  %s: %s",
			p.FromProvider, p.FromModel, p.ToProvider, p.ToModel))

	maxTokens := p.TokensFullReplay
	if t := p.TokensDistilled + p.TokensExtraction; t > maxTokens {
		maxTokens = t
	}

	replay := styles.Body.Render("full replay") + "\n" +
		bar(p.TokensFullReplay, maxTokens, styles.Fail) + "  " +
		styles.Dim.Render(fmt.Sprintf("%s tokens", comma(p.TokensFullReplay)))

	distilled := styles.Body.Render("distilled handoff") + "\n" +
		bar(p.TokensDistilled, maxTokens, styles.OK) + "  " +
		styles.Dim.Render(fmt.Sprintf("%s tokens", comma(p.TokensDistilled)))

	contents := styles.Ghost.Render(fmt.Sprintf("%d notes + %d recent turns",
		p.NotesCount, p.RawTurnCount))

	saving := lipgloss.NewStyle().Foreground(styles.OK).Bold(true).
		Render(fmt.Sprintf("%.1f%% smaller", p.Saving()))

	body := head + "\n\n" + replay + "\n\n" + distilled + "\n" + contents + "\n\n" + saving

	// Extraction cost is shown whenever it is non-zero, so the headline
	// percentage is never read without the cost that produced it.
	if p.TokensExtraction > 0 {
		body += styles.Ghost.Render(fmt.Sprintf("  (%.1f%% net of %s extraction tokens spent this session)",
			p.NetSaving(), comma(p.TokensExtraction)))
	}

	footer := styles.Key.Render("enter") + styles.Help.Render(" switch   ") +
		styles.Key.Render("esc") + styles.Help.Render(" cancel")

	modal := styles.Modal.Render(body + "\n\n" + footer)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

// comma groups thousands so five-digit token counts stay readable.
func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
