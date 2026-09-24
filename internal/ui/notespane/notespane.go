// Package notespane renders the distilled notes for the current session —
// small tagged records rather than a wall of text, per PRD Feature 5.
package notespane

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type keyMap struct {
	NextTag key.Binding
	Clear   key.Binding
}

var keys = keyMap{
	NextTag: key.NewBinding(key.WithKeys("t")),
	Clear:   key.NewBinding(key.WithKeys("T")),
}

type Model struct {
	vp      viewport.Model
	notes   []session.Note
	tags    []string // every distinct tag, for cycling
	filter  string   // active tag filter; empty means show all
	focused bool
	width   int
	height  int
	ready   bool
}

func New(notes []session.Note) Model {
	m := Model{}
	m.SetNotes(notes)
	return m
}

func (m *Model) SetNotes(notes []session.Note) {
	m.notes = notes
	m.tags = distinctTags(notes)
	// A filter for a tag that no longer exists would silently show nothing.
	if m.filter != "" && !contains(m.tags, m.filter) {
		m.filter = ""
	}
	if m.ready {
		m.vp.SetContent(m.render())
	}
}

// chromeRows is the height the frame takes that is not notes: two border rows,
// the title, the blank under it, and the hint line.
//
// The hint row is reserved whether or not the pane is focused. Showing it only
// on focus would make the pane change height as the user tabs through, and
// everything around it would jump.
const chromeRows = 5

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	iw, ih := styles.Inner(w), h-chromeRows
	if ih < 1 {
		ih = 1
	}
	if !m.ready {
		m.vp = viewport.New(iw, ih)
		m.ready = true
	} else {
		m.vp.Width, m.vp.Height = iw, ih
	}
	m.vp.SetContent(m.render())
}

func (m *Model) Focus()       { m.focused = true }
func (m *Model) Blur()        { m.focused = false }
func (m Model) Focused() bool { return m.focused }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, keys.NextTag):
			m.filter = nextTag(m.tags, m.filter)
			m.vp.SetContent(m.render())
			m.vp.GotoTop()
			return m, nil
		case key.Matches(km, keys.Clear):
			m.filter = ""
			m.vp.SetContent(m.render())
			m.vp.GotoTop()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) visible() []session.Note {
	var out []session.Note
	for _, n := range m.notes {
		// Superseded notes are excluded here for the same reason handoff
		// assembly excludes them: a replaced decision is no longer true.
		if !n.Active() {
			continue
		}
		if m.filter != "" && !contains(n.Tags, m.filter) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m Model) render() string {
	notes := m.visible()
	if len(notes) == 0 {
		if m.filter != "" {
			return styles.Ghost.Render("no notes tagged " + m.filter)
		}
		return styles.Ghost.Render("no notes extracted yet")
	}

	var b strings.Builder
	for i, n := range notes {
		b.WriteString(m.renderNote(n))
		if i < len(notes)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func (m Model) renderNote(n session.Note) string {
	w := styles.Inner(m.width)
	body := lipgloss.NewStyle().
		Width(w).
		Foreground(styles.Text).
		Render("• " + n.Content)

	var chips []string
	for _, t := range n.Tags {
		chips = append(chips, styles.Tag.Render(t))
	}
	tags := "  " + strings.Join(chips, " ")
	source := styles.Ghost.Render(n.Provider)

	// Put the source beside the tags only when both fit. Otherwise it gets its
	// own line; clipping it to "anth" would be worse than taking a row.
	if len(chips) == 0 {
		return body + "\n  " + source
	}
	if lipgloss.Width(tags)+2+lipgloss.Width(source) <= w {
		return body + "\n" + tags + "  " + source
	}
	return body + "\n" + tags + "\n  " + source
}

func (m Model) View() string {
	title := styles.PaneTitle("NOTES", m.focused)
	if m.filter != "" {
		title += styles.Dim.Render(" / " + m.filter)
	}
	if !m.ready {
		return styles.Frame(title, m.width, m.height, m.focused)
	}
	title += styles.Ghost.Render(fmt.Sprintf("  %d", len(m.visible())))

	hint := ""
	if m.focused && len(m.tags) > 0 {
		hint = styles.Key.Render("t") + styles.Help.Render(" filter by tag")
	}

	return styles.Frame(title+"\n\n"+m.vp.View()+"\n"+hint, m.width, m.height, m.focused)
}

func distinctTags(notes []session.Note) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range notes {
		for _, t := range n.Tags {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// nextTag cycles through the tags and back to "no filter", so one key can
// reach every state without a menu.
func nextTag(tags []string, current string) string {
	if len(tags) == 0 {
		return ""
	}
	if current == "" {
		return tags[0]
	}
	for i, t := range tags {
		if t == current {
			if i+1 < len(tags) {
				return tags[i+1]
			}
			return ""
		}
	}
	return ""
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
