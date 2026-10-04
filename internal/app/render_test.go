package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Keys that reach the provider list from the composer, where focus starts.
var toProviders = []string{"tab"}

func TestRendersAllPanesAtStandardSize(t *testing.T) {
	out := render(t, 120, 32)
	for _, want := range []string{"PROVIDERS", "TRANSCRIPT", "NOTES", "MESSAGE", "Anthropic", "wsl"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
}

// The divider is the visible evidence of a mid-session switch (SRS FR-3.6).
func TestTranscriptShowsSwitchDivider(t *testing.T) {
	if !strings.Contains(render(t, 120, 32), "switched to ollama") {
		t.Error("no provider-switch divider in the transcript")
	}
}

// The transcript must open on the newest turn, not the oldest.
func TestTranscriptOpensOnLatestMessage(t *testing.T) {
	if !strings.Contains(render(t, 120, 32), "while preserving the invariant") {
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
	out := render(t, 120, 32, append(toProviders, "s")...)
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
	out := render(t, 120, 32, append(toProviders, "s", "tab")...)
	if !strings.Contains(out, "Switch provider") {
		t.Error("modal closed or lost focus on a key it should have swallowed")
	}
}

func TestEscapeClosesModal(t *testing.T) {
	out := render(t, 120, 32, append(toProviders, "s", "esc")...)
	if strings.Contains(out, "Switch provider") {
		t.Error("modal still open after cancel")
	}
}

// Every frame must be exactly the terminal's height, or the terminal scrolls
// and the layout tears.
func TestFrameIsExactlyTerminalHeight(t *testing.T) {
	for _, size := range sizes {
		w, h := size[0], size[1]
		overlays := [][]string{
			append(toProviders, "s"),         // switch confirmation
			append(toProviders, "down", "r"), // removal confirmation
			{"tab", "tab", "x"},              // export form
		}
		for _, keys := range append([][]string{nil, {"tab"}, {"tab", "tab"}}, overlays...) {
			out := render(t, w, h, keys...)
			if got := len(strings.Split(out, "\n")); got != h {
				t.Errorf("at %dx%d after %v: frame is %d rows, want %d", w, h, keys, got, h)
			}
		}
	}
}

// No frame may exceed the terminal width, or lines wrap and the layout tears.
func TestFrameNeverExceedsTerminalWidth(t *testing.T) {
	for _, size := range sizes {
		w, h := size[0], size[1]
		for i, line := range strings.Split(render(t, w, h), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("at %dx%d line %d is %d cells wide, limit %d", w, h, i, got, w)
			}
		}
	}
}

var sizes = [][2]int{{80, 24}, {100, 30}, {120, 32}, {160, 40}, {60, 20}}

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
//
// The middle column is two panes stacked, transcript over composer, so this
// also checks that their heights sum to exactly the body height.
func TestPaneBordersAreIntact(t *testing.T) {
	top := regexp.MustCompile(`^(╭─+╮)+$`)
	bottom := regexp.MustCompile(`^(╰─+╯)+$`)

	check := func(label, frame string, w, h int) {
		t.Helper()
		lines := strings.Split(frame, "\n")
		first := ansiSGR.ReplaceAllString(lines[0], "")
		last := ansiSGR.ReplaceAllString(lines[h-3], "") // above status + help
		if !top.MatchString(first) {
			t.Errorf("%dx%d %s: top border broken: %q", w, h, label, first)
		}
		if !bottom.MatchString(last) {
			t.Errorf("%dx%d %s: bottom border broken: %q", w, h, label, last)
		}
	}

	for _, size := range sizes {
		w, h := size[0], size[1]
		for _, keys := range [][]string{nil, {"tab"}, {"tab", "tab"}, {"tab", "tab", "tab"}} {
			check(strings.Join(keys, ","), render(t, w, h, keys...), w, h)
		}

		// A pending reply adds the spinner line; a failed one adds a notice.
		// Neither may change the frame's size.
		pending := newHarness(t, w, h)
		pending.typeText("hello")
		pending.hold("enter")
		check("pending", pending.view(), w, h)

		failed := newHarness(t, w, h)
		failed.typeText("hello")
		failed.hold("enter")
		failed.press("esc")
		check("notice", failed.view(), w, h)

		// The composer's title names the active model, and some model ids
		// are long enough to wrap it.
		check("long model id", newHarness(t, w, h, withLongModel()).view(), w, h)
	}
}

// withLongModel makes the active provider's model id longer than the
// composer's title has room for at any supported size.
func withLongModel() option {
	return func(_ *Deps, s *State) {
		for i := range s.Providers {
			if s.Providers[i].Active {
				s.Providers[i].Model = "meta-llama/llama-3.1-70b-instruct:extended-context"
			}
		}
	}
}

// A model id too long for the composer's title is shortened, not wrapped.
func TestLongModelIdIsShortenedInTheComposerTitle(t *testing.T) {
	out := newHarness(t, 80, 24, withLongModel()).view()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "MESSAGE") {
			if !strings.Contains(line, "…") {
				t.Errorf("long model id not shortened: %q", line)
			}
			return
		}
	}
	t.Fatal("no composer title in the frame")
}

// Moving focus must not change any pane's size.
func TestFocusDoesNotShiftLayout(t *testing.T) {
	base := len(strings.Split(render(t, 120, 32), "\n"))
	for _, keys := range [][]string{{"tab"}, {"tab", "tab"}, {"tab", "tab", "tab"}} {
		if got := len(strings.Split(render(t, 120, 32, keys...), "\n")); got != base {
			t.Errorf("after %v frame height changed from %d to %d", keys, base, got)
		}
	}
}

// Narrow terminals drop sidebars rather than crushing the transcript.
func TestNarrowTerminalDegradesGracefully(t *testing.T) {
	out := render(t, 60, 20)
	for _, want := range []string{"TRANSCRIPT", "MESSAGE"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s should survive at 60 columns", want)
		}
	}
	for _, gone := range []string{"NOTES", "PROVIDERS"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s should be dropped at 60 columns", gone)
		}
	}
}

// DUMP=1 go test ./internal/app/ -run TestDump -v  prints a frame to look at.
// DUMP_KEYS="tab s" drives it first; DUMP_TYPE="hi" types into the composer
// and DUMP_HOLD=1 leaves the resulting request pending.
func TestDump(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("set DUMP=1 to print a frame")
	}
	h := newHarness(t, 120, 32)
	h.press(strings.Fields(os.Getenv("DUMP_KEYS"))...)
	if s := os.Getenv("DUMP_TYPE"); s != "" {
		h.typeText(s)
		if os.Getenv("DUMP_HOLD") != "" {
			h.hold("enter")
		} else {
			h.press("enter")
		}
	}
	t.Log("\n" + h.view())
}
