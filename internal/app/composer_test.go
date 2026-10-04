package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Dolmaa24/llmctl/internal/mock"
	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
)

func lastMessage(h *harness) session.Message {
	msgs := h.model().messages
	return msgs[len(msgs)-1]
}

func TestComposerHasFocusOnStart(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.typeText("hi")
	if got := h.model().composer.Value(); got != "hi" {
		t.Errorf("composer holds %q, want %q", got, "hi")
	}
}

func TestSendingAppendsMessageAndAttributedReply(t *testing.T) {
	h := newHarness(t, 120, 32)
	before := len(h.model().messages)

	h.typeText("How do I test the CAS loop?")
	h.press("enter")

	msgs := h.model().messages
	if len(msgs) != before+2 {
		t.Fatalf("got %d messages, want %d (the question and its reply)", len(msgs), before+2)
	}
	q, a := msgs[before], msgs[before+1]
	if q.Role != session.RoleUser || q.Content != "How do I test the CAS loop?" {
		t.Errorf("user turn = %+v", q)
	}
	// The reply must be attributed to the active provider (SRS FR-3.3).
	if a.Role != session.RoleAssistant || a.Provider != "ollama" || a.Model != "gemma3:latest" {
		t.Errorf("reply attributed to %s: %s, want ollama: gemma3:latest", a.Provider, a.Model)
	}
	if a.SequenceNum != q.SequenceNum+1 {
		t.Errorf("reply sequence %d does not follow question %d", a.SequenceNum, q.SequenceNum)
	}
	if h.model().composer.Value() != "" {
		t.Error("composer was not cleared after sending")
	}
	if !strings.Contains(h.view(), "How do I test the CAS loop?") {
		t.Error("the sent message does not appear in the transcript")
	}
}

// While the composer has focus, letters are text. "q" must not quit and "s"
// must not open the switch modal halfway through a sentence.
func TestLettersInComposerAreText(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.typeText("quick question, should I switch?")
	if h.quitRequested() {
		t.Fatal("typing q in the composer quit the program")
	}
	if h.model().confirm.Visible() {
		t.Fatal("typing s in the composer opened the switch modal")
	}
	if got := h.model().composer.Value(); got != "quick question, should I switch?" {
		t.Errorf("composer holds %q", got)
	}
}

func TestCtrlCQuitsFromComposer(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.typeText("half a thought")
	h.press("ctrl+c")
	if !h.quitRequested() {
		t.Error("ctrl+c did not quit while the composer had focus")
	}
}

// Ctrl+C must quit while the switch modal is open. It used not to: the modal
// swallowed every key, leaving no way out but killing the terminal.
func TestCtrlCQuitsFromTheSwitchModal(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "s", "ctrl+c")
	if !h.quitRequested() {
		t.Error("ctrl+c did not quit while the switch modal was open")
	}
}

func TestBlankMessageIsNotSent(t *testing.T) {
	h := newHarness(t, 120, 32)
	before := len(h.model().messages)
	h.press("enter")
	h.typeText("   ")
	h.press("enter")
	if got := len(h.model().messages); got != before {
		t.Errorf("a blank message was sent: %d messages, want %d", got, before)
	}
}

// Ctrl+J is the newline key the help bar shows; Alt+Enter keeps working for
// terminals that pass it through.
func TestNewlineKeysInsertNewlineWithoutSending(t *testing.T) {
	for _, k := range []string{"ctrl+j", "alt+enter"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, 120, 32)
			before := len(h.model().messages)
			h.typeText("first line")
			h.press(k)
			h.typeText("second line")
			if got := h.model().composer.Value(); got != "first line\nsecond line" {
				t.Errorf("composer holds %q", got)
			}
			if got := len(h.model().messages); got != before {
				t.Errorf("%s sent the message", k)
			}
		})
	}
}

// Windows Terminal takes Alt+Enter for full screen by default, so the help
// bar must point at Ctrl+J instead.
func TestHelpBarShowsCtrlJForNewline(t *testing.T) {
	out := newHarness(t, 120, 32).view()
	if !strings.Contains(out, "ctrl+j newline") {
		t.Errorf("help bar does not show ctrl+j for newline:\n%s", out)
	}
	if strings.Contains(out, "alt+enter") {
		t.Errorf("help bar still advertises alt+enter:\n%s", out)
	}
}

// The user can draft their next message while waiting, but Enter must not send
// it until the reply has arrived, or two requests would interleave.
func TestEnterWhileWaitingKeepsTheDraft(t *testing.T) {
	h := newHarness(t, 120, 32)
	before := len(h.model().messages)

	h.typeText("first")
	h.hold("enter") // request now in flight, not yet answered
	h.typeText("second")
	h.hold("enter") // must be ignored

	if got := len(h.model().messages); got != before+1 {
		t.Fatalf("got %d messages while waiting, want %d", got, before+1)
	}
	if got := h.model().composer.Value(); got != "second" {
		t.Fatalf("draft lost while waiting: composer holds %q", got)
	}

	h.release() // the first reply arrives
	h.press("enter")
	msgs := h.model().messages
	if got := len(msgs); got != before+4 {
		t.Fatalf("got %d messages, want %d", got, before+4)
	}
	if msgs[before+2].Content != "second" {
		t.Errorf("the draft was not sent after the reply arrived")
	}
}

// Esc must free the composer immediately, even when the adapter ignores
// cancellation and answers anyway. That late answer must be dropped rather
// than slipped into the conversation after the user moved on.
func TestEscCancelsEvenWhenAdapterIgnoresIt(t *testing.T) {
	stubborn := &mock.Adapter{
		NameValue: "ollama",
		SendMessageFn: func(ctx context.Context, model string, _ []session.Message) (session.Message, error) {
			// Ignores ctx entirely and replies as if nothing happened.
			return session.Message{Role: session.RoleAssistant, Content: "late reply"}, nil
		},
	}
	h := newHarness(t, 120, 32, withAdapters(stubborn))
	before := len(h.model().messages)

	h.typeText("hello")
	h.hold("enter")
	h.press("esc")
	h.release() // the adapter now answers, ignoring the cancellation

	if got := lastMessage(h); got.Content == "late reply" {
		t.Fatal("a reply for a cancelled request was appended")
	}
	if got := len(h.model().messages); got != before+1 {
		t.Errorf("got %d messages, want %d", got, before+1)
	}
	if n := h.model().transcript.Notice(); n != "request cancelled" {
		t.Errorf("notice = %q", n)
	}

	// The composer must be usable again at once.
	h.typeText("again")
	h.press("enter")
	if got := lastMessage(h); got.Content != "late reply" || got.Provider != "ollama" {
		t.Errorf("follow-up request did not complete normally: %+v", got)
	}
}

// A well-behaved adapter sees the cancellation through its context.
func TestEscCancelsTheAdaptersContext(t *testing.T) {
	var sawCancel bool
	polite := &mock.Adapter{
		NameValue: "ollama",
		SendMessageFn: func(ctx context.Context, _ string, _ []session.Message) (session.Message, error) {
			<-ctx.Done()
			sawCancel = errors.Is(ctx.Err(), context.Canceled)
			return session.Message{}, ctx.Err()
		},
	}
	h := newHarness(t, 120, 32, withAdapters(polite))
	h.typeText("hello")
	h.hold("enter")
	h.press("esc")
	h.release()
	if !sawCancel {
		t.Error("the adapter's context was not cancelled")
	}
}

// A provider failure is reported and the program carries on (SRS NFR-5).
func TestProviderErrorShowsNoticeAndKeepsRunning(t *testing.T) {
	h := newHarness(t, 120, 32)
	// Switch to OpenRouter, whose key the demo marks as expired.
	h.press("tab", "down", "s", "enter", "shift+tab")
	before := len(h.model().messages)

	h.typeText("hello")
	h.press("enter")

	if got := len(h.model().messages); got != before+1 {
		t.Errorf("got %d messages, want %d: a failed request must not add a reply", got, before+1)
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "openrouter") || !strings.Contains(n, "key expired") {
		t.Errorf("notice = %q, want the provider and the reason", n)
	}
	if h.model().inFlight {
		t.Error("request still marked in flight after failing")
	}
}

// A conversation too long for the model gets a notice that names the model
// and says what to do, not the bare sentinel text. The window size is shown
// when the adapter supplies it, and the program carries on either way.
func TestContextTooLargeNamesTheModelAndTheWayOut(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		detail string // expected in the notice; empty when the adapter gave none
	}{
		{"bare sentinel", provider.ErrContextTooLarge, ""},
		{"with the window size", fmt.Errorf("%w: needs 9214 of 8192 tokens", provider.ErrContextTooLarge), "(needs 9214 of 8192 tokens)"},
		{"wrapped by the adapter", fmt.Errorf("ollama: %w", provider.ErrContextTooLarge), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, 120, 32, withAdapters(&mock.Adapter{NameValue: "ollama", SendErr: c.err}))
			before := len(h.model().messages)

			h.typeText("hello")
			h.press("enter")

			n := h.model().transcript.Notice()
			for _, want := range []string{"ollama gemma3:latest", "too long for this model's context window", "press s to switch"} {
				if !strings.Contains(n, want) {
					t.Errorf("notice = %q, want it to contain %q", n, want)
				}
			}
			if strings.Contains(n, provider.ErrContextTooLarge.Error()) {
				t.Errorf("notice repeats the sentinel text: %q", n)
			}
			if c.detail != "" && !strings.Contains(n, c.detail) {
				t.Errorf("notice = %q, want the adapter's detail %q", n, c.detail)
			}
			if got := len(h.model().messages); got != before+1 {
				t.Errorf("got %d messages, want %d: a refused request must not add a reply", got, before+1)
			}
			if h.model().inFlight {
				t.Error("request still marked in flight after the refusal")
			}
		})
	}
}

// Error notices are shown to the user but are not conversation. They must
// never be sent to a model as part of the history.
func TestNoticesAreNotSentAsHistory(t *testing.T) {
	var history []session.Message
	recorder := &mock.Adapter{
		NameValue: "ollama",
		SendMessageFn: func(_ context.Context, _ string, h []session.Message) (session.Message, error) {
			history = h
			return session.Message{Role: session.RoleAssistant, Content: "ok"}, nil
		},
	}
	h := newHarness(t, 120, 32, withAdapters(recorder))
	h.typeText("first")
	h.hold("enter")
	h.press("esc") // produces a "request cancelled" notice
	h.release()
	h.typeText("second")
	h.press("enter")

	for _, m := range history {
		if strings.Contains(m.Content, "cancelled") {
			t.Fatalf("a notice was sent to the model as history: %+v", m)
		}
	}
}

// One adapter panicking must not take the program down (SRS NFR-5).
func TestAdapterPanicIsContained(t *testing.T) {
	broken := &mock.Adapter{
		NameValue: "ollama",
		SendMessageFn: func(context.Context, string, []session.Message) (session.Message, error) {
			panic("nil pointer in someone's adapter")
		},
	}
	h := newHarness(t, 120, 32, withAdapters(broken))
	h.typeText("hello")
	h.press("enter")
	if n := h.model().transcript.Notice(); !strings.Contains(n, "panicked") {
		t.Errorf("notice = %q, want the panic reported", n)
	}
	if h.model().inFlight {
		t.Error("request still in flight after the adapter panicked")
	}
}

func TestSwitchChangesWhereTheNextMessageGoes(t *testing.T) {
	h := newHarness(t, 120, 32)
	// Cursor starts on Anthropic, which is not the active provider.
	h.press("tab", "s", "enter", "shift+tab")

	h.typeText("back to the frontier model")
	h.press("enter")

	if got := lastMessage(h); got.Provider != "anthropic" || got.Model != "claude-opus-5" {
		t.Errorf("reply came from %s: %s, want anthropic: claude-opus-5", got.Provider, got.Model)
	}
	if !strings.Contains(h.view(), "switched to anthropic") {
		t.Error("no divider marking the switch back to anthropic")
	}
}

// The divider is drawn while the reply is pending, so the transcript does not
// jump a line when the reply lands.
func TestDividerAppearsBeforeTheReply(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "s", "enter", "shift+tab")
	h.typeText("hello")
	h.hold("enter")

	out := h.view()
	if !strings.Contains(out, "switched to anthropic") {
		t.Error("divider missing while the reply is pending")
	}
	if !strings.Contains(out, "thinking") {
		t.Error("no pending indicator while waiting")
	}
}

func TestSwitchingIsBlockedWhileWaiting(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.typeText("hello")
	h.hold("enter", "tab", "s")
	if h.model().confirm.Visible() {
		t.Error("switch modal opened with a request in flight")
	}
}

func TestEnterOnAProviderOpensTheSwitch(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "enter")
	if !h.model().confirm.Visible() {
		t.Error("enter on a provider did not open the switch modal")
	}
}

func TestSwitchingToTheActiveProviderDoesNothing(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "down", "down", "s") // Ollama, already active
	if h.model().confirm.Visible() {
		t.Error("modal offered a switch to the provider already in use")
	}
}

// On a narrow terminal, Tab must skip the panes that are not on screen.
func TestFocusSkipsHiddenPanes(t *testing.T) {
	h := newHarness(t, 60, 20) // providers and notes are hidden here
	h.press("tab")
	if h.model().focus != paneTranscript {
		t.Errorf("focus went to hidden pane %d", h.model().focus)
	}
	h.press("tab")
	if h.model().focus != paneComposer {
		t.Errorf("focus went to hidden pane %d", h.model().focus)
	}
}
