// Package styles holds every Lip Gloss definition the TUI uses.
//
// Panes must not define their own colours or borders. Keeping them here is
// what stops five panes built at different times from drifting into five
// slightly different looks, and it is the single place to change when the
// theme is adjusted.
package styles

import "github.com/charmbracelet/lipgloss"

// Palette. AdaptiveColor picks the right variant for light and dark
// terminals, so the TUI stays readable in either without a theme setting.
var (
	Text    = lipgloss.AdaptiveColor{Light: "#1c1c1c", Dark: "#e4e4e4"}
	Muted   = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#8a8a8a"}
	Faint   = lipgloss.AdaptiveColor{Light: "#9a9a9a", Dark: "#5f5f5f"}
	Border  = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3a3a3a"}
	Accent  = lipgloss.AdaptiveColor{Light: "#b35300", Dark: "#e08a3c"}
	Surface = lipgloss.AdaptiveColor{Light: "#f4f4f4", Dark: "#242424"}

	OK   = lipgloss.AdaptiveColor{Light: "#2f7d32", Dark: "#6fbf73"}
	Warn = lipgloss.AdaptiveColor{Light: "#a86a00", Dark: "#e0a33c"}
	Fail = lipgloss.AdaptiveColor{Light: "#b3261e", Dark: "#f2726a"}
)

// Base text styles.
var (
	Title = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	Body  = lipgloss.NewStyle().Foreground(Text)
	Dim   = lipgloss.NewStyle().Foreground(Muted)
	Ghost = lipgloss.NewStyle().Foreground(Faint)
)

// Pane returns the border style for a pane, emphasised when it holds focus.
// Focus is shown by border colour rather than a different border character,
// so the layout does not shift by a cell when focus moves.
func Pane(focused bool) lipgloss.Style {
	c := Border
	if focused {
		c = Accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Padding(0, 1)
}

// Frame draws content inside a pane border so the result is exactly w columns
// wide and h rows tall, border included.
//
// Every pane must render through this rather than calling Pane().Width()
// itself. Lip Gloss's Width excludes the border but includes the padding, so
// passing a content width re-wraps each line two columns narrower than it was
// wrapped for: the last word of every line falls onto its own row, the pane
// grows taller than its slot, and the columns stop lining up.
func Frame(content string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	// MaxHeight is a safety net for the running UI, not a fix: if content ever
	// overflows, one pane loses its bottom border instead of the whole screen
	// scrolling. It also hides the overflow from a row count, which is why the
	// tests check border integrity instead.
	return Pane(focused).
		Width(w - 2).
		Height(h - 2).
		MaxHeight(h).
		Render(content)
}

// Inner returns the text width available inside a Frame of outer width w: one
// border column and one padding column on each side.
func Inner(w int) int {
	if w < 5 {
		return 1
	}
	return w - 4
}

// PaneTitle renders a pane's heading.
func PaneTitle(s string, focused bool) string {
	if focused {
		return Title.Render(s)
	}
	return lipgloss.NewStyle().Foreground(Muted).Bold(true).Render(s)
}

// StatusColour maps a doctor status string to its colour. It takes a string
// rather than doctor.Status so the ui packages do not depend on the doctor
// package for rendering.
func StatusColour(status string) lipgloss.AdaptiveColor {
	switch status {
	case "ok":
		return OK
	case "warn":
		return Warn
	case "fail":
		return Fail
	default:
		return Muted
	}
}

// Attribution renders a "provider: model" tag for a transcript turn.
var Attribution = lipgloss.NewStyle().Foreground(Accent).Bold(true)

// Selected marks the highlighted row in a list.
var Selected = lipgloss.NewStyle().Foreground(Accent).Bold(true)

// Tag renders a note tag chip.
var Tag = lipgloss.NewStyle().
	Foreground(Muted).
	Background(Surface).
	Padding(0, 1)

// Divider renders the rule shown in the transcript at a provider switch.
var Divider = lipgloss.NewStyle().Foreground(Accent)

// Modal is the frame for an overlay such as the switch confirmation.
var Modal = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(Accent).
	Padding(1, 3)

// Key renders a keybinding hint, Help the text beside it.
var (
	Key  = lipgloss.NewStyle().Foreground(Accent)
	Help = lipgloss.NewStyle().Foreground(Faint)
)
