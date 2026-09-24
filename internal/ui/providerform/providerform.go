// Package providerform is where the user adds a provider or changes one.
//
// The schema allows one profile per provider type, so the same form serves
// both: choosing a type that is not configured yet adds it, and choosing one
// that is prefills its settings to update it. Removing a provider is not done
// here; it needs its own confirmation step (SRS NFR-8).
//
// The API key is masked while typed, never rendered in any state, and
// cleared from the form when it closes.
package providerform

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Kind is a provider type the form can configure.
type Kind struct {
	ID        string // "anthropic"
	Name      string // "Anthropic"
	NeedsKey  bool
	BaseURL   string // default
	Model     string // default; may be empty
	ModelHint string // placeholder shown when there is no default model
}

// Saved describes a provider that is already configured, for prefilling.
type Saved struct {
	BaseURL string
	Model   string
	HasKey  bool
}

// Result is what the user asked to save.
type Result struct {
	Kind    Kind
	BaseURL string
	Model   string
	APIKey  string // empty: keep the key already saved
}

// Messages to the root model. The form performs no side effects: it reports
// what the user asked for, and the root model validates and saves.
type (
	// SubmitMsg asks for the result to be validated and saved. Force is set
	// when the user chose to save despite a failed validation.
	SubmitMsg struct {
		Result Result
		Force  bool
	}
	// StopMsg asks for a validation in progress to be abandoned.
	StopMsg struct{}
	// DismissMsg reports that the user closed the form.
	DismissMsg struct{}
)

type state int

const (
	editing state = iota
	validating
	failed
	saved
)

type field int

const (
	fieldKind field = iota
	fieldKey
	fieldURL
	fieldModel
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
	kinds   []Kind
	saved   map[string]Saved
	kindIdx int

	key, url, model textinput.Model
	focus           field

	state   state
	message string // detail for failed and saved states
	problem string // why the input cannot be submitted yet

	spin    spinner.Model
	visible bool
	width   int
	height  int
}

func New() Model {
	return Model{
		key:   newInput(true),
		url:   newInput(false),
		model: newInput(false),
		spin: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(styles.Attribution),
		),
	}
}

func newInput(secret bool) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Cursor.SetMode(cursor.CursorStatic)
	in.PlaceholderStyle = styles.Ghost
	in.TextStyle = styles.Body
	if secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	return in
}

// Open shows the form. preselect names the provider to start on and moves
// focus straight to its key, since changing a key is the usual reason to open
// an existing provider. With no preselect, the first provider not yet
// configured is chosen and focus starts on the provider choice.
func (m *Model) Open(kinds []Kind, saved map[string]Saved, preselect string) {
	m.kinds, m.saved = kinds, saved
	m.visible = true
	m.state, m.message, m.problem = editing, "", ""

	m.kindIdx = 0
	m.focus = fieldKind
	if i := m.indexOf(preselect); i >= 0 {
		m.kindIdx = i
		m.focus = fieldKey
	} else {
		for i, k := range kinds {
			if _, ok := saved[k.ID]; !ok {
				m.kindIdx = i
				break
			}
		}
	}
	m.applyKind()
	if !m.fieldShown(m.focus) {
		m.focus = m.nextField(m.focus, +1)
	}
	m.focusField()
}

// Close hides the form and clears every input. The key in particular must not
// sit in memory after the user has left the form.
func (m *Model) Close() {
	m.visible = false
	m.state, m.message, m.problem = editing, "", ""
	m.key.Reset()
	m.url.Reset()
	m.model.Reset()
}

func (m Model) Visible() bool { return m.visible }

// KeyValue is exposed for tests that assert the key is cleared on close.
func (m Model) KeyValue() string { return m.key.Value() }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	iw := m.formWidth() - 8 - labelWidth - 1 // frame, padding, label, cursor
	if iw < 8 {
		iw = 8
	}
	m.key.Width, m.url.Width, m.model.Width = iw, iw, iw
}

// SetValidating shows that a validation is running, and returns the command
// that starts the spinner.
func (m *Model) SetValidating() tea.Cmd {
	m.state, m.message, m.problem = validating, "", ""
	return m.spin.Tick
}

// SetFailed shows why validation failed. The user can fix a field and try
// again, or save anyway.
func (m *Model) SetFailed(reason string) {
	m.state, m.message = failed, reason
}

// SetSaved shows that the provider was saved.
func (m *Model) SetSaved(detail string) {
	m.state, m.message = saved, detail
	m.key.Reset() // saved; the form has no further use for it
	m.key.Placeholder = "saved"
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	if tick, ok := msg.(spinner.TickMsg); ok {
		if m.state != validating {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(tick)
		return m, cmd
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.state {
	case validating:
		// Fields are locked while a validation runs, so what is saved is what
		// was validated. Esc abandons the validation and unlocks them.
		if key.Matches(km, keys.Back) {
			m.state, m.message = editing, ""
			m.problem = "validation stopped"
			return m, func() tea.Msg { return StopMsg{} }
		}
		return m, nil

	case saved:
		if key.Matches(km, keys.Submit) || key.Matches(km, keys.Back) {
			return m, func() tea.Msg { return DismissMsg{} }
		}
		return m, nil
	}

	// editing or failed
	switch {
	case key.Matches(km, keys.Back):
		return m, func() tea.Msg { return DismissMsg{} }

	case key.Matches(km, keys.Submit):
		return m.submit()

	case key.Matches(km, keys.Next):
		m.focus = m.nextField(m.focus, +1)
		m.focusField()
		return m, nil

	case key.Matches(km, keys.Prev):
		m.focus = m.nextField(m.focus, -1)
		m.focusField()
		return m, nil

	case m.focus == fieldKind && (key.Matches(km, keys.Left) || key.Matches(km, keys.Right)):
		step := 1
		if key.Matches(km, keys.Left) {
			step = -1
		}
		m.kindIdx = (m.kindIdx + step + len(m.kinds)) % len(m.kinds)
		m.applyKind()
		m.edited()
		return m, nil
	}

	// Anything else is typing into the focused input.
	var cmd tea.Cmd
	switch m.focus {
	case fieldKey:
		before := m.key.Value()
		m.key, cmd = m.key.Update(km)
		if m.key.Value() != before {
			m.edited()
		}
	case fieldURL:
		before := m.url.Value()
		m.url, cmd = m.url.Update(km)
		if m.url.Value() != before {
			m.edited()
		}
	case fieldModel:
		before := m.model.Value()
		m.model, cmd = m.model.Update(km)
		if m.model.Value() != before {
			m.edited()
		}
	}
	return m, cmd
}

// edited returns a failed form to plain editing. After a change, Enter should
// validate the new values, not save the old ones anyway.
func (m *Model) edited() {
	m.problem = ""
	if m.state == failed {
		m.state, m.message = editing, ""
	}
}

func (m Model) submit() (Model, tea.Cmd) {
	k := m.kinds[m.kindIdx]
	res := Result{
		Kind: k,
		// Keys are nearly always pasted, and a paste often brings a trailing
		// newline or space that would make a valid key fail to authenticate.
		APIKey:  strings.TrimSpace(m.key.Value()),
		BaseURL: strings.TrimRight(strings.TrimSpace(m.url.Value()), "/"),
		Model:   strings.TrimSpace(m.model.Value()),
	}

	if p := problemWith(res, m.saved[k.ID].HasKey); p != "" {
		m.problem = p
		return m, nil
	}
	force := m.state == failed
	return m, func() tea.Msg { return SubmitMsg{Result: res, Force: force} }
}

// problemWith checks what can be checked without connecting to anything.
func problemWith(r Result, hasSavedKey bool) string {
	if r.Kind.NeedsKey && r.APIKey == "" && !hasSavedKey {
		return "an API key is required"
	}
	if r.BaseURL == "" {
		return "a base URL is required"
	}
	u, err := url.Parse(r.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "the base URL must start with http:// or https://"
	}
	if r.Model == "" {
		return "a model is required"
	}
	return ""
}

// applyKind loads the selected provider's settings: its saved values if it is
// configured, otherwise its defaults. The key is always cleared, so a key
// typed for one provider can never be submitted as another's.
func (m *Model) applyKind() {
	k := m.kinds[m.kindIdx]
	m.key.SetValue("")
	if s, ok := m.saved[k.ID]; ok {
		m.url.SetValue(s.BaseURL)
		m.model.SetValue(s.Model)
	} else {
		m.url.SetValue(k.BaseURL)
		m.model.SetValue(k.Model)
	}
	m.url.CursorEnd()
	m.model.CursorEnd()
	m.model.Placeholder = k.ModelHint

	m.key.Placeholder = "paste your API key"
	if m.saved[k.ID].HasKey {
		m.key.Placeholder = "saved · leave blank to keep it"
	}
}

func (m Model) indexOf(id string) int {
	for i, k := range m.kinds {
		if k.ID == id {
			return i
		}
	}
	return -1
}

func (m Model) fieldShown(f field) bool {
	return f != fieldKey || m.kinds[m.kindIdx].NeedsKey
}

func (m Model) nextField(from field, step int) field {
	f := from
	for i := 0; i < int(fieldCount); i++ {
		f = field((int(f) + step + int(fieldCount)) % int(fieldCount))
		if m.fieldShown(f) {
			return f
		}
	}
	return from
}

func (m *Model) focusField() {
	m.key.Blur()
	m.url.Blur()
	m.model.Blur()
	switch m.focus {
	case fieldKey:
		m.key.Focus()
	case fieldURL:
		m.url.Focus()
	case fieldModel:
		m.model.Focus()
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
	if !m.visible || len(m.kinds) == 0 {
		return ""
	}
	k := m.kinds[m.kindIdx]

	title := "Add provider"
	if _, ok := m.saved[k.ID]; ok {
		title = "Update provider"
	}

	rows := []string{styles.Title.Render(title), ""}
	rows = append(rows, m.row(fieldKind, "Provider", m.kindChoice()))
	rows = append(rows, "")
	if k.NeedsKey {
		rows = append(rows, m.row(fieldKey, "API key", m.key.View()))
	}
	rows = append(rows,
		m.row(fieldURL, "Base URL", m.url.View()),
		m.row(fieldModel, "Model", m.model.View()),
		"",
		m.statusLine(),
		"",
		m.footer(),
	)

	inner := m.formWidth() - 8
	body := lipgloss.NewStyle().Width(inner).Render(strings.Join(rows, "\n"))
	form := styles.Modal.Width(m.formWidth() - 2).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, form)
}

func (m Model) row(f field, label, value string) string {
	l := styles.Dim.Render(fmt.Sprintf("%-*s", labelWidth, label))
	if m.focus == f && (m.state == editing || m.state == failed) {
		l = styles.Selected.Render(fmt.Sprintf("%-*s", labelWidth, label))
	}
	return l + value
}

// kindChoice renders the provider choice as radio buttons. The filled circle
// marks the choice by shape as well as colour.
func (m Model) kindChoice() string {
	var parts []string
	for i, k := range m.kinds {
		if i == m.kindIdx {
			style := styles.Body
			if m.focus == fieldKind {
				style = styles.Selected
			}
			parts = append(parts, style.Render("● "+k.Name))
		} else {
			parts = append(parts, styles.Dim.Render("○ "+k.Name))
		}
	}
	return strings.Join(parts, "   ")
}

func (m Model) statusLine() string {
	name := m.kinds[m.kindIdx].Name
	switch m.state {
	case validating:
		return m.spin.View() + styles.Dim.Render(" connecting to "+name+"…")
	case failed:
		return lipgloss.NewStyle().Foreground(styles.Fail).Render("✕ " + m.message)
	case saved:
		return lipgloss.NewStyle().Foreground(styles.OK).Render("✓ " + m.message)
	}
	if m.problem != "" {
		return lipgloss.NewStyle().Foreground(styles.Warn).Render(m.problem)
	}
	return styles.Ghost.Render("Saving checks the connection first.")
}

func (m Model) footer() string {
	var pairs [][2]string
	switch m.state {
	case validating:
		pairs = [][2]string{{"esc", "stop"}}
	case failed:
		pairs = [][2]string{{"enter", "save anyway"}, {"tab", "next field"}, {"esc", "cancel"}}
	case saved:
		pairs = [][2]string{{"enter", "done"}}
	default:
		pairs = [][2]string{{"enter", "save"}, {"tab", "next field"}}
		if m.focus == fieldKind {
			pairs = append(pairs, [2]string{"←→", "provider"})
		}
		pairs = append(pairs, [2]string{"esc", "cancel"})
	}
	var out []string
	for _, p := range pairs {
		out = append(out, styles.Key.Render(p[0])+styles.Help.Render(" "+p[1]))
	}
	return strings.Join(out, styles.Help.Render("   "))
}
