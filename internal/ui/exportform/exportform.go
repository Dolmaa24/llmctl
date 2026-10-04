// Package exportform is where the user exports the current session as a
// Markdown or JSON file (SRS FR-5.3).
//
// It performs no side effects. It reports the format and file name the user
// chose, and the root model renders the session and writes the file.
package exportform

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Format is an export file format.
type Format int

const (
	Markdown Format = iota
	JSON
	formatCount
)

func (f Format) String() string {
	if f == JSON {
		return "JSON"
	}
	return "Markdown"
}

// Ext is the file extension for f, dot included.
func (f Format) Ext() string {
	if f == JSON {
		return ".json"
	}
	return ".md"
}

// Messages to the root model.
type (
	// SubmitMsg asks for the session to be written to Name in Format.
	SubmitMsg struct {
		Format Format
		Name   string
	}
	// DismissMsg reports that the user closed the form.
	DismissMsg struct{}
)

type state int

const (
	editing state = iota
	writing
	failed
)

type field int

const (
	fieldFormat field = iota
	fieldName
	fieldCount
)

var keys = struct {
	Next, Prev, Left, Right, Submit, Back key.Binding
}{
	Next:   key.NewBinding(key.WithKeys("tab", "down")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab", "up")),
	Left:   key.NewBinding(key.WithKeys("left")),
	Right:  key.NewBinding(key.WithKeys("right")),
	Submit: key.NewBinding(key.WithKeys("enter")),
	Back:   key.NewBinding(key.WithKeys("esc")),
}

const (
	maxWidth   = 66
	labelWidth = 10
)

type Model struct {
	format Format
	name   textinput.Model
	dir    string // where a relative name is written, shown so nothing lands somewhere unexpected
	focus  field

	state   state
	message string // why the last export failed
	problem string // why the input cannot be submitted yet

	visible bool
	width   int
	height  int
}

func New() Model {
	in := textinput.New()
	in.Prompt = ""
	in.Cursor.SetMode(cursor.CursorStatic)
	in.PlaceholderStyle = styles.Ghost
	in.TextStyle = styles.Body
	in.Placeholder = "file name"
	return Model{name: in}
}

// Open shows the form with a suggested file name, given without its
// extension so the extension can follow the chosen format. dir is where a
// relative name will be written.
func (m *Model) Open(stem, dir string) {
	m.visible = true
	m.state, m.message, m.problem = editing, "", ""
	m.format = Markdown
	m.dir = dir
	m.name.SetValue(stem + m.format.Ext())
	m.name.CursorEnd()
	m.focus = fieldFormat
	m.focusField()
}

// Close hides the form and clears it.
func (m *Model) Close() {
	m.visible = false
	m.state, m.message, m.problem = editing, "", ""
	m.name.Reset()
}

func (m Model) Visible() bool { return m.visible }

// Format and Name are exposed for tests.
func (m Model) Format() Format { return m.format }
func (m Model) Name() string   { return m.name.Value() }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	iw := m.formWidth() - 8 - labelWidth - 1 // frame, padding, label, cursor
	if iw < 8 {
		iw = 8
	}
	m.name.Width = iw
}

// SetWriting shows that the export is being written.
func (m *Model) SetWriting() { m.state, m.message, m.problem = writing, "", "" }

// SetFailed shows why the export failed. The user can change the name or
// format and try again.
func (m *Model) SetFailed(reason string) { m.state, m.message = failed, reason }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.state == writing {
		// Fields are locked while the file is written. Esc closes the form;
		// the root model still reports how the export ended.
		if key.Matches(km, keys.Back) {
			return m, func() tea.Msg { return DismissMsg{} }
		}
		return m, nil
	}

	switch {
	case key.Matches(km, keys.Back):
		return m, func() tea.Msg { return DismissMsg{} }

	case key.Matches(km, keys.Submit):
		return m.submit()

	case key.Matches(km, keys.Next), key.Matches(km, keys.Prev):
		// Two fields: either direction moves to the other one.
		m.focus = (m.focus + 1) % fieldCount
		m.focusField()
		return m, nil

	case m.focus == fieldFormat && (key.Matches(km, keys.Left) || key.Matches(km, keys.Right)):
		m.setFormat((m.format + 1) % formatCount)
		m.edited()
		return m, nil
	}

	if m.focus != fieldName {
		return m, nil
	}
	before := m.name.Value()
	var cmd tea.Cmd
	m.name, cmd = m.name.Update(km)
	if m.name.Value() != before {
		m.edited()
	}
	return m, cmd
}

// setFormat changes the format, and the extension with it when the name still
// ends in the old format's extension. A name the user gave an extension of
// their own is left alone.
func (m *Model) setFormat(f Format) {
	name := m.name.Value()
	if strings.HasSuffix(name, m.format.Ext()) {
		m.name.SetValue(strings.TrimSuffix(name, m.format.Ext()) + f.Ext())
		m.name.CursorEnd()
	}
	m.format = f
}

// edited returns a failed form to plain editing, so the message about the
// old name does not sit beside a new one.
func (m *Model) edited() {
	m.problem = ""
	if m.state == failed {
		m.state, m.message = editing, ""
	}
}

func (m Model) submit() (Model, tea.Cmd) {
	name := strings.TrimSpace(m.name.Value())
	if name == "" {
		m.problem = "a file name is required"
		return m, nil
	}
	f := m.format
	return m, func() tea.Msg { return SubmitMsg{Format: f, Name: name} }
}

func (m *Model) focusField() {
	m.name.Blur()
	if m.focus == fieldName {
		m.name.Focus()
	}
}

func (m Model) formWidth() int {
	w := m.width - 4
	if w > maxWidth {
		w = maxWidth
	}
	if w < 40 {
		w = 40
	}
	return w
}

func (m Model) View() string {
	if !m.visible {
		return ""
	}
	rows := []string{
		styles.Title.Render("Export session"),
		styles.Dim.Render("The transcript and notes of this session."),
		"",
		m.row(fieldFormat, "Format", m.formatChoice()),
		styles.Dim.Render(fmt.Sprintf("%-*s", labelWidth, "Folder")) + styles.Ghost.Render(trimLeft(m.dir, m.name.Width)),
		m.row(fieldName, "File", m.name.View()),
		"",
		m.statusLine(),
		"",
		m.footer(),
	}
	inner := m.formWidth() - 8
	body := lipgloss.NewStyle().Width(inner).Render(strings.Join(rows, "\n"))
	form := styles.Modal.Width(m.formWidth() - 2).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, form)
}

func (m Model) row(f field, label, value string) string {
	l := styles.Dim.Render(fmt.Sprintf("%-*s", labelWidth, label))
	if m.focus == f && m.state != writing {
		l = styles.Selected.Render(fmt.Sprintf("%-*s", labelWidth, label))
	}
	return l + value
}

// formatChoice renders the formats as radio buttons. The filled circle marks
// the choice by shape as well as colour.
func (m Model) formatChoice() string {
	var parts []string
	for f := Format(0); f < formatCount; f++ {
		if f == m.format {
			style := styles.Body
			if m.focus == fieldFormat {
				style = styles.Selected
			}
			parts = append(parts, style.Render("● "+f.String()))
		} else {
			parts = append(parts, styles.Dim.Render("○ "+f.String()))
		}
	}
	return strings.Join(parts, "   ")
}

func (m Model) statusLine() string {
	switch m.state {
	case writing:
		return styles.Dim.Render("writing…")
	case failed:
		return lipgloss.NewStyle().Foreground(styles.Fail).Render("✕ " + m.message)
	}
	if m.problem != "" {
		return lipgloss.NewStyle().Foreground(styles.Warn).Render(m.problem)
	}
	return styles.Ghost.Render("A full path saves elsewhere. An existing file is never replaced.")
}

// trimLeft shortens s to w cells by dropping its start, so the end of a long
// path, the part that tells folders apart, stays visible on one line.
func trimLeft(s string, w int) string {
	r := []rune(s)
	if w < 2 || len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-(w-1):])
}

func (m Model) footer() string {
	var pairs [][2]string
	switch m.state {
	case writing:
		pairs = [][2]string{{"esc", "close"}}
	case failed:
		pairs = [][2]string{{"enter", "try again"}, {"tab", "next field"}, {"esc", "cancel"}}
	default:
		pairs = [][2]string{{"enter", "export"}, {"tab", "next field"}}
		if m.focus == fieldFormat {
			pairs = append(pairs, [2]string{"←→", "format"})
		}
		pairs = append(pairs, [2]string{"esc", "cancel"})
	}
	var out []string
	for _, p := range pairs {
		out = append(out, styles.Key.Render(p[0])+styles.Help.Render(" "+p[1]))
	}
	return strings.Join(out, styles.Help.Render("   "))
}
