package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// HandoffPlan is what will actually be sent to the new provider on a switch:
// the accumulated non-superseded notes plus the last N raw turns. Building it
// sends nothing — it is a pure planning step so the UI can show the cost
// comparison before the user confirms.
type HandoffPlan struct {
	Notes          []Note
	RecentRawTurns []Message

	EstimatedTokensFullReplay int
	EstimatedTokensDistilled  int
}

// HandoffBuilder assembles a HandoffPlan for a session.
type HandoffBuilder interface {
	BuildHandoff(ctx context.Context, sess *Session) (HandoffPlan, error)
}

// Payload returns exactly the messages a switch sends: one system message
// carrying the notes, then the recent turns verbatim.
//
// The builder estimates the distilled cost from Payload, and the switch sends
// Payload. Because both go through this one function, the figure shown before
// a switch is the figure billed after it. If the two were produced separately
// they could drift apart, and the comparison this project exists to make would
// quietly stop being true.
//
// The notes travel as a system message because they are context supplied by
// the tool, not something the user said. Mapping it onto each provider's wire
// format — Anthropic takes system text as a top-level field — is the
// adapter's job.
func (p HandoffPlan) Payload() []Message {
	out := make([]Message, 0, len(p.RecentRawTurns)+1)
	if len(p.Notes) > 0 {
		out = append(out, Message{Role: RoleSystem, Content: renderNotes(p.Notes)})
	}
	return append(out, p.RecentRawTurns...)
}

// renderNotes writes the notes as the text of the handoff message. Every
// token here is paid on every switch, so the framing is kept to one line.
//
// Notes are atomic facts and are rendered one per line; a newline inside one
// is folded to a space so it cannot break the list.
func renderNotes(notes []Note) string {
	var b strings.Builder
	b.WriteString("Notes carried over from earlier in this conversation, distilled rather than the full transcript. The most recent messages follow verbatim.\n")
	for _, n := range notes {
		b.WriteString("\n- ")
		if len(n.Tags) > 0 {
			b.WriteString("[" + strings.Join(n.Tags, ", ") + "] ")
		}
		b.WriteString(strings.Join(strings.Fields(n.Content), " "))
	}
	return b.String()
}

// DefaultRecentTurns is how many recent messages travel verbatim.
//
// It is provisional. PRD open question #5 asks what this should be, and the
// evaluation answers it by measuring retention and cost across a sweep of
// values rather than by picking one; this default holds until then. Four
// messages is the last two exchanges.
const DefaultRecentTurns = 2

// The builder's dependencies are declared here, as the small interfaces it
// actually uses, rather than imported. Package storage imports session for
// its types, and so does costestimate, so importing either from here would be
// an import cycle. The storage repositories and costestimate.Heuristic satisfy
// these without knowing they exist.

// MessageLister is satisfied by storage.MessagesRepo.
type MessageLister interface {
	ListBySession(ctx context.Context, sessionID string) ([]Message, error)
}

// NoteLister is satisfied by storage.NotesRepo.
type NoteLister interface {
	ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]Note, error)
}

// TokenCounter is satisfied by costestimate.Estimator.
type TokenCounter interface {
	CountMessages(msgs []Message) int
}

// Builder is the HandoffBuilder: accumulated non-superseded notes plus the
// most recent turns.
type Builder struct {
	messages MessageLister
	notes    NoteLister
	counter  TokenCounter
	recent   int
}

var _ HandoffBuilder = (*Builder)(nil)

// NewBuilder returns a Builder that carries the given number of recent
// messages verbatim. Zero sends notes alone.
func NewBuilder(messages MessageLister, notes NoteLister, counter TokenCounter, recent int) *Builder {
	if recent < 0 {
		recent = 0
	}
	return &Builder{messages: messages, notes: notes, counter: counter, recent: recent}
}

func (b *Builder) BuildHandoff(ctx context.Context, sess *Session) (HandoffPlan, error) {
	if sess == nil {
		return HandoffPlan{}, errors.New("session: cannot build a handoff for a nil session")
	}
	msgs, err := b.messages.ListBySession(ctx, sess.ID)
	if err != nil {
		return HandoffPlan{}, fmt.Errorf("session: loading messages for handoff: %w", err)
	}
	notes, err := b.notes.ListBySession(ctx, sess.ID, false)
	if err != nil {
		return HandoffPlan{}, fmt.Errorf("session: loading notes for handoff: %w", err)
	}

	// Order by sequence number rather than trusting whatever order storage
	// returned: the recent window is only correct on an ordered transcript.
	msgs = append([]Message(nil), msgs...)
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].SequenceNum < msgs[j].SequenceNum })

	plan := HandoffPlan{RecentRawTurns: recentWindow(msgs, b.recent)}

	// When the window already reaches back to the first message, the notes
	// could only repeat what is being sent verbatim, at extra cost. A short
	// session is then handed over whole: lossless, and no dearer than replay.
	if len(plan.RecentRawTurns) < len(msgs) {
		plan.Notes = activeNotes(notes)
	}

	plan.EstimatedTokensFullReplay = b.counter.CountMessages(msgs)
	plan.EstimatedTokensDistilled = b.counter.CountMessages(plan.Payload())
	return plan, nil
}

// recentWindow returns the last n messages, widened backwards so it begins on
// a user message. A window opening on a reply would hand the new model an
// answer without its question, and most provider APIs reject a conversation
// whose first message is the assistant's.
func recentWindow(msgs []Message, n int) []Message {
	if n <= 0 || len(msgs) == 0 {
		return nil
	}
	start := len(msgs) - n
	if start < 0 {
		start = 0
	}
	for start > 0 && msgs[start].Role != RoleUser {
		start--
	}
	return append([]Message(nil), msgs[start:]...)
}

// activeNotes drops superseded notes and orders the rest chronologically.
//
// NotesRepo is asked for non-superseded notes already; filtering again costs
// nothing and means a repository that ignores the flag cannot carry a
// reversed decision into a new provider.
func activeNotes(notes []Note) []Note {
	var out []Note
	for _, n := range notes {
		if n.Active() {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
