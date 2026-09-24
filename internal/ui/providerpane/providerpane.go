// Package providerpane renders the configured providers, their health and the
// active model.
package providerpane

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Item is one row in the pane. It is a view-model, deliberately not
// config.ProviderConfig: the pane renders provider health and latency, which
// are not part of stored configuration, and it should not have to know which
// package each field came from.
type Item struct {
	ID       string // "anthropic"
	Name     string // "Anthropic"
	Model    string // active model id
	Status   string // "ok" | "warn" | "fail" | ""
	LatencyM int    // milliseconds; 0 when unknown
	Active   bool   // the provider this session is currently using
}

// SelectedMsg is emitted when the user activates a provider. The root model
// turns this into a switch; the pane itself performs no side effects.
type SelectedMsg struct{ ID string }

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Select: key.NewBinding(key.WithKeys("enter")),
}

type Model struct {
	items   []Item
	cursor  int
	focused bool
	width   int
	height  int
}

func New(items []Item) Model {
	return Model{items: items}
}

func (m *Model) SetItems(items []Item) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }
func (m *Model) Focus()           { m.focused = true }
func (m *Model) Blur()            { m.focused = false }
func (m Model) Focused() bool     { return m.focused }

// Items returns the rows the pane is rendering. The root model reads these
// rather than holding a second copy that could drift out of step.
func (m Model) Items() []Item { return m.items }

// Selected returns the highlighted item and whether there was one.
func (m Model) Selected() (Item, bool) {
	if len(m.items) == 0 {
		return Item{}, false
	}
	return m.items[m.cursor], true
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case key.Matches(msg, keys.Select):
			if item, ok := m.Selected(); ok {
				id := item.ID
				return m, func() tea.Msg { return SelectedMsg{ID: id} }
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(styles.PaneTitle("PROVIDERS", m.focused))
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(styles.Ghost.Render("none configured"))
		return styles.Frame(b.String(), m.width, m.height, m.focused)
	}

	for i, it := range m.items {
		b.WriteString(m.renderItem(i, it))
		if i < len(m.items)-1 {
			b.WriteString("\n\n")
		}
	}
	return styles.Frame(b.String(), m.width, m.height, m.focused)
}

// renderItem draws two lines: the cursor gutter, health dot and name, then the
// model id with latency. Everything is measured against the content width so
// nothing wraps inside the frame.
func (m Model) renderItem(i int, it Item) string {
	w := styles.Inner(m.width)

	gutter := "  "
	name := styles.Body.Render(it.Name)
	if i == m.cursor && m.focused {
		gutter = styles.Selected.Render("› ")
		name = styles.Selected.Render(it.Name)
	}
	dot := lipgloss.NewStyle().Foreground(styles.StatusColour(it.Status)).Render(statusDot(it.Status))

	// "active" is a word, not a second dot: two dots on one line read as two
	// health indicators.
	line1 := gutter + dot + " " + name
	if it.Active {
		line1 += styles.Dim.Render(" · active")
	}

	latency := ""
	if it.LatencyM > 0 {
		latency = fmt.Sprintf("%dms", it.LatencyM)
	}
	const indent = "    "
	room := w - len(indent)
	if latency != "" {
		room -= len(latency) + 1
	}
	model := truncate(it.Model, room)
	line2 := styles.Dim.Render(indent + model)
	if latency != "" {
		pad := w - lipgloss.Width(indent+model) - len(latency)
		if pad < 1 {
			pad = 1
		}
		line2 += strings.Repeat(" ", pad) + styles.Ghost.Render(latency)
	}

	return line1 + "\n" + line2
}

func statusDot(status string) string {
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

func truncate(s string, w int) string {
	if w <= 1 || len(s) <= w {
		return s
	}
	if w < 2 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
