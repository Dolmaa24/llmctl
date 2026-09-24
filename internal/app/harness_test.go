package app

import (
	"testing"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/ui/composer"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// harness drives the model the way the Bubble Tea runtime does, but
// deterministically, so tests can stop a request mid-flight and act while it
// is pending.
type harness struct {
	t     *testing.T
	m     tea.Model
	queue []tea.Cmd
	seen  []tea.Msg

	// While holding, the work a submission starts — the request to the
	// provider — is parked here instead of queued, leaving it in flight.
	holding bool
	parked  []tea.Cmd
}

func newHarness(t *testing.T, w, h int, reg *provider.Registry) *harness {
	t.Helper()
	if reg == nil {
		reg = DemoRegistry(0)
	}
	providers, messages, notes, checks := DemoState()
	hh := &harness{t: t, m: New(reg, providers, messages, notes, checks)}
	hh.deliver(tea.WindowSizeMsg{Width: w, Height: h})
	hh.run()
	return hh
}

// deliver hands one message to the model and queues the command it returns
// without running it.
func (h *harness) deliver(msg tea.Msg) {
	var cmd tea.Cmd
	h.m, cmd = h.m.Update(msg)
	if cmd == nil {
		return
	}
	// Enter does not start a request directly: it produces a SubmitMsg, and
	// handling that is what starts one. So parking has to key off the
	// submission. Stopping after a fixed number of steps instead would leave
	// the submission itself queued, with nothing yet in flight.
	if _, ok := msg.(composer.SubmitMsg); ok && h.holding {
		h.parked = append(h.parked, cmd)
		return
	}
	h.queue = append(h.queue, cmd)
}

// run executes queued commands and feeds their results back in until nothing
// is left, expanding batches as the runtime does. Spinner ticks are dropped
// rather than fed back: each schedules the next, so the loop would never end.
func (h *harness) run() {
	h.t.Helper()
	for budget := 500; len(h.queue) > 0; budget-- {
		if budget == 0 {
			h.t.Fatal("command queue never drained")
		}
		cmd := h.queue[0]
		h.queue = h.queue[1:]
		msg := cmd()
		if msg == nil {
			continue
		}
		h.seen = append(h.seen, msg)
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				if c != nil {
					h.queue = append(h.queue, c)
				}
			}
			continue
		case spinner.TickMsg:
			continue
		}
		h.deliver(msg)
	}
}

// press delivers each key and lets everything it started finish.
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.deliver(keyMsg(k))
		h.run()
	}
}

// hold delivers keys and lets their immediate effects finish, but parks any
// request a submission starts, so it is left in flight.
func (h *harness) hold(keys ...string) {
	h.t.Helper()
	h.holding = true
	defer func() { h.holding = false }()
	for _, k := range keys {
		h.deliver(keyMsg(k))
		h.run()
	}
}

// release lets parked requests run to completion.
func (h *harness) release() {
	h.t.Helper()
	h.queue = append(h.queue, h.parked...)
	h.parked = nil
	h.run()
}

// typeText types s one character at a time, as a user would.
func (h *harness) typeText(s string) {
	for _, r := range s {
		h.deliver(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) view() string { return h.m.View() }
func (h *harness) model() Model { return h.m.(Model) }

func (h *harness) quitRequested() bool {
	for _, msg := range h.seen {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "alt+enter":
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// render is the one-line form for tests that only need a frame.
func render(t *testing.T, w, h int, keys ...string) string {
	t.Helper()
	hh := newHarness(t, w, h, nil)
	hh.press(keys...)
	return hh.view()
}
