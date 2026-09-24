package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// render drives the model the way Bubble Tea would: size it, feed it keys,
// then ask for a frame.
func render(t *testing.T, w, h int, keys ...string) string {
	t.Helper()
	providers, messages, notes, checks := DemoState()
	var m tea.Model = New(providers, messages, notes, checks)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		var msg tea.Msg
		switch k {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		// Sub-models report decisions by returning a command; run it and feed
		// the result back, exactly as the runtime does.
		if cmd != nil {
			if out := cmd(); out != nil {
				m, _ = m.Update(out)
			}
		}
	}
	return m.View()
}

func TestRendersAllPanesAtStandardSize(t *testing.T) {
	out := render(t, 120, 32)
	for _, want := range []string{"PROVIDERS", "TRANSCRIPT", "NOTES", "Anthropic", "wsl"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
}

// The divider is the visible evidence of a mid-session switch (SRS FR-3.6).
func TestTranscriptShowsSwitchDivider(t *testing.T) {
	out := render(t, 120, 32)
	if !strings.Contains(out, "switched to ollama") {
		t.Error("no provider-switch divider in the transcript")
	}
}

// The transcript must open on the newest turn, not the oldest.
func TestTranscriptOpensOnLatestMessage(t *testing.T) {
	out := render(t, 120, 32)
	if !strings.Contains(out, "while preserving the invariant") {
		t.Error("the end of the latest message is not visible on first render")
	}
}

// A superseded note is no longer true, so it must not be shown.
func TestSupersededNoteIsHidden(t *testing.T) {
	out := render(t, 120, 32)
	if strings.Contains(out, "Suspected a data race") {
		t.Error("superseded note is being rendered")
	}
	if !strings.Contains(out, "Ruled out") {
		t.Error("the note that superseded it should still be shown")
	}
}

func TestSwitchModalOpensAndShowsBothCosts(t *testing.T) {
	out := render(t, 120, 32, "down", "s")
	for _, want := range []string{"Switch provider", "full replay", "distilled handoff", "14,500", "2,800"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal is missing %q", want)
		}
	}
	// The saving must never appear without the extraction cost behind it.
	if !strings.Contains(out, "extraction tokens") {
		t.Error("modal shows a saving without disclosing extraction cost")
	}
}

func TestModalCapturesKeysWhileOpen(t *testing.T) {
	// Tab would move focus if the modal were not exclusive.
	out := render(t, 120, 32, "down", "s", "tab")
	if !strings.Contains(out, "Switch provider") {
		t.Error("modal closed or lost focus on a key it should have swallowed")
	}
}

func TestEscapeClosesModal(t *testing.T) {
	out := render(t, 120, 32, "down", "s", "n")
	if strings.Contains(out, "Switch provider") {
		t.Error("modal still open after cancel")
	}
}

// No frame may exceed the terminal it was given, or the display will wrap and
// the layout will tear.
func TestFrameNeverExceedsTerminalWidth(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 32}, {160, 40}, {60, 20}} {
		w, h := size[0], size[1]
		out := render(t, w, h)
		for i, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("at %dx%d line %d is %d cells wide, limit %d", w, h, i, got, w)
			}
		}
	}
}

// Every frame must be exactly the terminal's height. Taller and the terminal
// scrolls, tearing the layout; this is the check whose absence let the first
// version ship with every pane overflowing its slot.
func TestFrameIsExactlyTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 32}, {160, 40}, {60, 20}} {
		w, h := size[0], size[1]
		for _, keys := range [][]string{nil, {"tab"}, {"tab", "tab"}, {"down", "s"}} {
			out := render(t, w, h, keys...)
			if got := len(strings.Split(out, "\n")); got != h {
				t.Errorf("at %dx%d after %v: frame is %d rows, want %d", w, h, keys, got, h)
			}
		}
	}
}

// ansiSGR matches the colour sequences Lip Gloss emits, so border checks are
// not thrown off when a colour profile is active.
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// The first and last rows of the body must be unbroken pane borders from the
// first column to the last.
//
// This is the check that catches a mis-sized pane. The obvious checks do not:
// line width misses a pane that renders too narrow, because JoinVertical pads
// every row to the widest line and the shortfall hides as trailing spaces; and
// row count misses a pane that renders too tall, because Frame's MaxHeight
// clips it back to size. Both failures show up here — a narrow pane leaves
// spaces after its last corner, and a clipped pane loses its bottom border.
func TestPaneBordersAreIntact(t *testing.T) {
	top := regexp.MustCompile(`^(╭─+╮)+$`)
	bottom := regexp.MustCompile(`^(╰─+╯)+$`)

	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 32}, {160, 40}, {60, 20}} {
		w, h := size[0], size[1]
		for _, keys := range [][]string{nil, {"tab"}, {"tab", "tab"}} {
			lines := strings.Split(render(t, w, h, keys...), "\n")
			first := ansiSGR.ReplaceAllString(lines[0], "")
			last := ansiSGR.ReplaceAllString(lines[h-3], "") // above status + help

			if !top.MatchString(first) {
				t.Errorf("%dx%d %v: top border broken: %q", w, h, keys, first)
			}
			if !bottom.MatchString(last) {
				t.Errorf("%dx%d %v: bottom border broken: %q", w, h, keys, last)
			}
		}
	}
}

// Moving focus must not change any pane's size.
func TestFocusDoesNotShiftLayout(t *testing.T) {
	base := render(t, 120, 32)
	for _, keys := range [][]string{{"tab"}, {"tab", "tab"}} {
		out := render(t, 120, 32, keys...)
		if a, b := len(strings.Split(base, "\n")), len(strings.Split(out, "\n")); a != b {
			t.Errorf("after %v frame height changed from %d to %d", keys, a, b)
		}
	}
}

// Narrow terminals drop sidebars rather than crushing the transcript.
func TestNarrowTerminalDegradesGracefully(t *testing.T) {
	out := render(t, 60, 20)
	if !strings.Contains(out, "TRANSCRIPT") {
		t.Error("transcript should survive at 60 columns")
	}
	if strings.Contains(out, "NOTES") {
		t.Error("notes pane should be dropped at 60 columns")
	}
}

// DUMP=1 go test ./internal/app/ -run TestDump -v  prints a frame to look at.
// DUMP_KEYS="down s" drives it first, e.g. to open the switch modal.
func TestDump(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("set DUMP=1 to print a frame")
	}
	keys := strings.Fields(os.Getenv("DUMP_KEYS"))
	t.Log("\n" + render(t, 120, 32, keys...))
}
