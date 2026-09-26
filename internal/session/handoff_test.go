package session_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/costestimate"
	"github.com/Dolmaa24/llmctl/internal/mock"
	"github.com/Dolmaa24/llmctl/internal/session"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

var counter costestimate.Heuristic

// conversation builds n alternating messages, user first.
func conversation(sessionID string, n int, words int) []session.Message {
	var out []session.Message
	for i := 0; i < n; i++ {
		m := session.Message{
			SessionID:   sessionID,
			SequenceNum: i,
			Role:        session.RoleUser,
			Content:     fmt.Sprintf("message %d: %s", i, strings.Repeat("word ", words)),
			CreatedAt:   t0.Add(time.Duration(i) * time.Minute),
		}
		if i%2 == 1 {
			m.Role, m.Provider, m.Model = session.RoleAssistant, "anthropic", "claude-opus-5"
		}
		out = append(out, m)
	}
	return out
}

type fixture struct {
	msgs  *mock.MessagesRepo
	notes *mock.NotesRepo
	sess  *session.Session
}

func newFixture(t *testing.T, msgs []session.Message, notes []session.Note) fixture {
	t.Helper()
	f := fixture{
		msgs:  mock.NewMessagesRepo(),
		notes: mock.NewNotesRepo(),
		sess:  &session.Session{ID: "s1", ActiveProvider: "anthropic", ActiveModel: "claude-opus-5"},
	}
	ctx := context.Background()
	for i := range msgs {
		if err := f.msgs.Append(ctx, &msgs[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i := range notes {
		if err := f.notes.Create(ctx, &notes[i]); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f fixture) build(t *testing.T, recent int) session.HandoffPlan {
	t.Helper()
	b := session.NewBuilder(f.msgs, f.notes, costestimate.Heuristic{}, recent)
	plan, err := b.BuildHandoff(context.Background(), f.sess)
	if err != nil {
		t.Fatalf("BuildHandoff: %v", err)
	}
	return plan
}

func note(id, content string, minute int, tags ...string) session.Note {
	return session.Note{ID: id, SessionID: "s1", Content: content, Tags: tags,
		Provider: "anthropic", Model: "claude-opus-5", CreatedAt: t0.Add(time.Duration(minute) * time.Minute)}
}

// The figure shown before a switch must be the figure billed after it: the
// distilled estimate is exactly the count of what Payload sends.
func TestEstimateIsTheCostOfThePayload(t *testing.T) {
	f := newFixture(t, conversation("s1", 30, 40), []session.Note{
		note("n1", "decided: atomic CAS for the refill timer", 3, "decision"),
		note("n2", "the timer is guarded by one mutex", 1, "fact"),
	})
	plan := f.build(t, session.DefaultRecentTurns)

	want := counter.CountMessages(plan.Payload())
	if plan.EstimatedTokensDistilled != want {
		t.Errorf("estimated %d, but the payload costs %d", plan.EstimatedTokensDistilled, want)
	}
	all, _ := f.msgs.ListBySession(context.Background(), "s1")
	if full := counter.CountMessages(all); plan.EstimatedTokensFullReplay != full {
		t.Errorf("full replay estimated %d, but the transcript costs %d", plan.EstimatedTokensFullReplay, full)
	}
}

func TestLongSessionIsMuchCheaperDistilled(t *testing.T) {
	f := newFixture(t, conversation("s1", 30, 40), []session.Note{
		note("n1", "decided: atomic CAS for the refill timer", 3, "decision"),
		note("n2", "the timer is guarded by one mutex", 1, "fact"),
	})
	plan := f.build(t, session.DefaultRecentTurns)
	if plan.EstimatedTokensDistilled*3 > plan.EstimatedTokensFullReplay {
		t.Errorf("distilled %d is not well under replay %d for a 30-message session",
			plan.EstimatedTokensDistilled, plan.EstimatedTokensFullReplay)
	}
}

// A session that fits in the recent window is handed over whole. Adding notes
// would only repeat what is sent verbatim, so the honest result is no saving.
func TestShortSessionIsHandedOverWhole(t *testing.T) {
	msgs := conversation("s1", 3, 10)
	f := newFixture(t, msgs, []session.Note{note("n1", "a fact", 1, "fact")})
	plan := f.build(t, session.DefaultRecentTurns)

	if len(plan.Notes) != 0 {
		t.Errorf("notes sent alongside the whole transcript: %d", len(plan.Notes))
	}
	if len(plan.RecentRawTurns) != 3 {
		t.Errorf("sent %d of 3 messages", len(plan.RecentRawTurns))
	}
	if plan.EstimatedTokensDistilled != plan.EstimatedTokensFullReplay {
		t.Errorf("distilled %d != replay %d for a session sent whole",
			plan.EstimatedTokensDistilled, plan.EstimatedTokensFullReplay)
	}
}

// The window must open on a user message, never on an orphaned reply.
func TestRecentWindowStartsOnAUserMessage(t *testing.T) {
	msgs := conversation("s1", 10, 5) // user, assistant, ... ends on assistant
	f := newFixture(t, msgs, nil)

	for _, n := range []int{1, 2, 3, 4, 5} {
		plan := f.build(t, n)
		if len(plan.RecentRawTurns) < n {
			t.Errorf("n=%d: window shrank to %d", n, len(plan.RecentRawTurns))
		}
		if first := plan.RecentRawTurns[0]; first.Role != session.RoleUser {
			t.Errorf("n=%d: window opens on a %s message", n, first.Role)
		}
		if last := plan.RecentRawTurns[len(plan.RecentRawTurns)-1]; last.SequenceNum != 9 {
			t.Errorf("n=%d: window does not end on the latest message", n)
		}
	}
}

func TestZeroRecentTurnsSendsNotesAlone(t *testing.T) {
	f := newFixture(t, conversation("s1", 10, 5), []session.Note{note("n1", "a fact", 1)})
	plan := f.build(t, 0)
	payload := plan.Payload()
	if len(payload) != 1 || payload[0].Role != session.RoleSystem {
		t.Errorf("payload = %d messages, want the notes message alone", len(payload))
	}
}

// A reversed decision must never travel to the new provider, even if a
// repository ignores the request to leave superseded notes out.
func TestSupersededNotesNeverTravel(t *testing.T) {
	replaced := "n2"
	old := note("n1", "suspected a data race", 1, "hypothesis")
	old.SupersededBy = &replaced
	f := newFixture(t, conversation("s1", 20, 5), []session.Note{
		old, note("n2", "ruled out: it is lock contention", 2, "decision"),
	})

	b := session.NewBuilder(f.msgs, leakyNotes{f.notes}, costestimate.Heuristic{}, 4)
	plan, err := b.BuildHandoff(context.Background(), f.sess)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range plan.Notes {
		if n.ID == "n1" {
			t.Fatal("a superseded note was included in the handoff")
		}
	}
	if !strings.Contains(plan.Payload()[0].Content, "ruled out") {
		t.Error("the note that replaced it is missing")
	}
}

// leakyNotes ignores includeSuperseded, like a buggy repository would.
type leakyNotes struct{ r *mock.NotesRepo }

func (l leakyNotes) ListBySession(ctx context.Context, id string, _ bool) ([]session.Note, error) {
	return l.r.ListBySession(ctx, id, true)
}

func TestNotesAreChronological(t *testing.T) {
	f := newFixture(t, conversation("s1", 20, 5), []session.Note{
		note("n3", "third", 3), note("n1", "first", 1), note("n2", "second", 2),
	})
	plan := f.build(t, 4)
	var order []string
	for _, n := range plan.Notes {
		order = append(order, n.Content)
	}
	if got := strings.Join(order, ","); got != "first,second,third" {
		t.Errorf("notes in order %s", got)
	}
}

// Storage may return messages in any order; the window depends on sequence.
func TestMessagesAreOrderedBySequence(t *testing.T) {
	msgs := conversation("s1", 10, 5)
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	b := session.NewBuilder(&unordered{msgs}, mock.NewNotesRepo(), counter, 2)
	plan, err := b.BuildHandoff(context.Background(), &session.Session{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.RecentRawTurns[len(plan.RecentRawTurns)-1].SequenceNum; got != 9 {
		t.Errorf("window ends on message %d, want 9", got)
	}
}

type unordered struct{ msgs []session.Message }

func (u *unordered) ListBySession(context.Context, string) ([]session.Message, error) {
	return u.msgs, nil
}

func TestPayloadShape(t *testing.T) {
	plan := session.HandoffPlan{
		Notes: []session.Note{
			note("n1", "decided: atomic CAS", 1, "decision", "concurrency"),
			note("n2", "line one\nline two", 2),
		},
		RecentRawTurns: conversation("s1", 2, 3),
	}
	p := plan.Payload()
	if len(p) != 3 || p[0].Role != session.RoleSystem {
		t.Fatalf("payload = %+v", p)
	}
	body := p[0].Content
	if !strings.Contains(body, "- [decision, concurrency] decided: atomic CAS") {
		t.Errorf("tagged note rendered as:\n%s", body)
	}
	if !strings.Contains(body, "- line one line two") {
		t.Errorf("a multi-line note broke the list:\n%s", body)
	}
	if p[1].SequenceNum != 0 || p[2].SequenceNum != 1 {
		t.Error("recent turns out of order after the notes")
	}

	p[1].Content = "changed"
	if plan.RecentRawTurns[0].Content == "changed" {
		t.Error("Payload aliases the plan: changing it changed the plan")
	}
}

func TestPayloadWithoutNotesIsJustTheTurns(t *testing.T) {
	plan := session.HandoffPlan{RecentRawTurns: conversation("s1", 2, 3)}
	if p := plan.Payload(); len(p) != 2 || p[0].Role != session.RoleUser {
		t.Errorf("payload = %+v", p)
	}
}

func TestErrorsCarryContext(t *testing.T) {
	boom := errors.New("disk on fire")
	b := session.NewBuilder(failing{boom}, mock.NewNotesRepo(), costestimate.Heuristic{}, 4)
	_, err := b.BuildHandoff(context.Background(), &session.Session{ID: "s1"})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "loading messages") {
		t.Errorf("err = %v", err)
	}
	if _, err := b.BuildHandoff(context.Background(), nil); err == nil {
		t.Error("nil session accepted")
	}
}

type failing struct{ err error }

func (f failing) ListBySession(context.Context, string) ([]session.Message, error) {
	return nil, f.err
}

func TestSwitchEventRecordsWhatWasSent(t *testing.T) {
	f := newFixture(t, conversation("s1", 20, 5), []session.Note{note("n1", "a", 1), note("n2", "b", 2)})
	plan := f.build(t, 4)
	ev := session.NewSwitchEvent(f.sess, plan, "ollama", "llama3.1:8b", t0)

	if ev.FromProvider != "anthropic" || ev.ToProvider != "ollama" || ev.ToModel != "llama3.1:8b" {
		t.Errorf("route = %s:%s -> %s:%s", ev.FromProvider, ev.FromModel, ev.ToProvider, ev.ToModel)
	}
	if ev.NotesSentCount != 2 || ev.RawTurnsSentCount != len(plan.RecentRawTurns) {
		t.Errorf("counts = %d notes, %d turns", ev.NotesSentCount, ev.RawTurnsSentCount)
	}
	if ev.EstimatedTokensDistilled != plan.EstimatedTokensDistilled ||
		ev.EstimatedTokensFullReplay != plan.EstimatedTokensFullReplay {
		t.Error("event estimates differ from the plan's")
	}
}
