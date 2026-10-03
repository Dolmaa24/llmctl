package notes_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhairyaP4/llmctl/internal/mock"
	"github.com/DhairyaP4/llmctl/internal/notes"
	"github.com/DhairyaP4/llmctl/internal/session"
)

var bg = context.Background()

// rig is one session with in-memory storage and a fake model per provider.
type rig struct {
	t      *testing.T
	msgs   *mock.MessagesRepo
	lister session.MessageLister // what the runner reads through; msgs unless a test swaps it
	notes  *mock.NotesRepo
	store  notes.NoteStore // what the runner writes through; notes unless a test swaps it
	ledger notes.Ledger
	models map[string]notes.Sender
	sess   *session.Session
	next   int
}

func newRig(t *testing.T) *rig {
	r := &rig{
		t:      t,
		msgs:   mock.NewMessagesRepo(),
		notes:  mock.NewNotesRepo(),
		ledger: mock.NewExtractionRunsRepo(),
		models: map[string]notes.Sender{"anthropic": &fakeModel{reply: `{"notes": []}`}},
		sess:   &session.Session{ID: "s1", ActiveProvider: "anthropic", ActiveModel: opus},
	}
	r.lister, r.store = r.msgs, r.notes
	return r
}

func (r *rig) runner(s notes.Schedule) *notes.Runner {
	lookup := func(provider string) (notes.Sender, error) {
		if m, ok := r.models[provider]; ok {
			return m, nil
		}
		return nil, fmt.Errorf("no adapter registered for %q", provider)
	}
	return notes.NewRunner(s, r.lister, r.store, r.ledger, lookup, counter)
}

// exchange stores a question and the answer provider/model gave it.
func (r *rig) exchange(provider, model, question, answer string) {
	r.t.Helper()
	for _, m := range []session.Message{
		{Role: session.RoleUser, Content: question},
		{Role: session.RoleAssistant, Content: answer, Provider: provider, Model: model},
	} {
		m.SessionID, m.SequenceNum = r.sess.ID, r.next
		r.next++
		if err := r.msgs.Append(bg, &m); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *rig) model(provider string) *fakeModel { return r.models[provider].(*fakeModel) }

func (r *rig) runs() []session.ExtractionRun {
	runs, _ := r.ledger.ListBySession(bg, r.sess.ID)
	return runs
}

func (r *rig) stored() []session.Note {
	all, _ := r.notes.ListBySession(bg, r.sess.ID, true)
	return all
}

func TestContinuousTakesNotesAfterEachReply(t *testing.T) {
	rg := newRig(t)
	m := rg.model("anthropic")
	m.reply = `{"notes": [{"kind": "fact", "content": "The limiter lives in bucket.go", "source": "M2"}]}`
	rg.exchange("anthropic", opus, "Where is the limiter?", "In internal/ratelimit/bucket.go.")

	res, err := rg.runner(notes.Continuous).AfterReply(bg, rg.sess)
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || m.model != haiku {
		t.Fatalf("%d calls on %q, want one on the cheap model", m.calls, m.model)
	}
	stored := rg.stored()
	if len(stored) != 1 || len(res.Notes) != 1 || stored[0].ID != res.Notes[0].ID {
		t.Fatalf("stored %+v, returned %+v; want the one note, stored", stored, res.Notes)
	}
	runs := rg.runs()
	if len(runs) != 1 || runs[0].ThroughSequenceNum != 1 || runs[0].Err != "" || runs[0].Model != haiku ||
		!slices.Equal(runs[0].NoteIDs, []string{stored[0].ID}) {
		t.Errorf("runs = %+v, want one completed run through message 1 that produced the note", runs)
	}
}

func TestEachPassReadsOnlyTheNewTurns(t *testing.T) {
	rg := newRig(t)
	r, m := rg.runner(notes.Continuous), rg.model("anthropic")

	rg.exchange("anthropic", opus, "first question", "first answer")
	if _, err := r.AfterReply(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	rg.exchange("anthropic", opus, "second question", "second answer")
	if _, err := r.AfterReply(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 {
		t.Fatalf("%d calls, want one per reply", m.calls)
	}
	if p := m.prompts[1]; strings.Contains(p, "first question") || !strings.Contains(p, "second question") {
		t.Errorf("the second pass read the whole transcript, not just the new turns:\n%s", p)
	}

	if _, err := r.AfterReply(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 {
		t.Error("a pass ran with nothing new to read")
	}
}

func TestAtSwitchWaitsForTheSwitch(t *testing.T) {
	rg := newRig(t)
	r, m := rg.runner(notes.AtSwitch), rg.model("anthropic")
	for _, q := range []string{"first question", "second question"} {
		rg.exchange("anthropic", opus, q, "an answer")
		if _, err := r.AfterReply(bg, rg.sess); err != nil {
			t.Fatal(err)
		}
	}
	if m.calls != 0 || len(rg.runs()) != 0 {
		t.Fatalf("%d calls before any switch, want none", m.calls)
	}

	if _, err := r.BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || !strings.Contains(m.prompts[0], "first question") || !strings.Contains(m.prompts[0], "second question") {
		t.Errorf("%d calls; want one pass over everything at the switch", m.calls)
	}
}

func TestEveryFewRepliesBatchesPasses(t *testing.T) {
	rg := newRig(t)
	r, m := rg.runner(notes.Schedule(2)), rg.model("anthropic")

	rg.exchange("anthropic", opus, "first question", "an answer")
	if _, err := r.AfterReply(bg, rg.sess); err != nil || m.calls != 0 {
		t.Fatalf("after one reply: %d calls, err %v; want none yet", m.calls, err)
	}
	rg.exchange("anthropic", opus, "second question", "an answer")
	if _, err := r.AfterReply(bg, rg.sess); err != nil || m.calls != 1 {
		t.Fatalf("after two replies: %d calls, err %v; want one", m.calls, err)
	}
	if !strings.Contains(m.prompts[0], "first question") {
		t.Error("the batched pass skipped the first exchange")
	}
}

func TestAFailedPassIsChargedAndReadAgain(t *testing.T) {
	rg := newRig(t)
	r, m := rg.runner(notes.Continuous), rg.model("anthropic")
	m.reply = "I would rather not."
	rg.exchange("anthropic", opus, "first question", "an answer")

	if _, err := r.AfterReply(bg, rg.sess); err == nil {
		t.Fatal("want the unreadable reply reported")
	}
	runs := rg.runs()
	if len(runs) != 1 || runs[0].Err == "" || runs[0].InputTokens == 0 {
		t.Fatalf("runs = %+v, want one failed run, charged", runs)
	}

	m.reply = `{"notes": []}`
	if _, err := r.BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || !strings.Contains(m.prompts[1], "first question") {
		t.Errorf("the failed turns were not read again: %d calls", m.calls)
	}
}

func TestAReplacementIsStoredBeforeItIsReferenced(t *testing.T) {
	rg := newRig(t)
	old := note("n-race", "question", "Is it a data race?", 0)
	if err := rg.notes.Create(bg, &old); err != nil {
		t.Fatal(err)
	}
	rg.model("anthropic").reply = `{"notes": [{"kind": "fact", "content": "It is lock contention, not a race", "replaces": "N1"}]}`
	rg.exchange("anthropic", opus, "Is it a race?", "No: -race is clean. It is lock contention.")

	// The mock, like the real table, refuses to point at a note that
	// does not exist yet.
	res, err := rg.runner(notes.Continuous).AfterReply(bg, rg.sess)
	if err != nil {
		t.Fatal(err)
	}
	var got session.Note
	for _, n := range rg.stored() {
		if n.ID == old.ID {
			got = n
		}
	}
	if len(res.Notes) != 1 || got.SupersededBy == nil || *got.SupersededBy != res.Notes[0].ID {
		t.Errorf("old note superseded by %v, want %+v", got.SupersededBy, res.Notes)
	}
	if runs := rg.runs(); len(runs) != 1 {
		t.Errorf("runs = %+v, want one that overturned one note", runs)
	}
}

func TestProvidersAreNeverMixedInAPass(t *testing.T) {
	rg := newRig(t)
	local := &fakeModel{name: "ollama", reply: `{"notes": []}`}
	rg.models["ollama"] = local
	hosted := rg.model("anthropic")
	// Both sides of a switch are unread, as after a failed pass or a restart.
	rg.exchange("anthropic", opus, "asked of Opus", "answered by Opus")
	rg.exchange("ollama", "qwen2.5-coder:3b", "asked of qwen", "answered by qwen")

	if _, err := rg.runner(notes.AtSwitch).BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if hosted.calls != 1 || local.calls != 1 {
		t.Fatalf("calls: hosted %d, local %d; want one pass each", hosted.calls, local.calls)
	}
	if strings.Contains(hosted.prompts[0], "qwen") {
		t.Errorf("the hosted model was shown the local conversation:\n%s", hosted.prompts[0])
	}
	if strings.Contains(local.prompts[0], "Opus") || !strings.Contains(local.prompts[0], "asked of qwen") {
		t.Errorf("the local pass read the wrong turns:\n%s", local.prompts[0])
	}
	if hosted.model != haiku || local.model != "llama3.1:8b" {
		t.Errorf("models: hosted %q, local %q", hosted.model, local.model)
	}
	if runs := rg.runs(); len(runs) != 2 || runs[0].ThroughSequenceNum != 1 || runs[1].ThroughSequenceNum != 3 {
		t.Errorf("runs = %+v, want one per provider, in order", runs)
	}
}

func TestALocalPassUsesTheDedicatedModel(t *testing.T) {
	rg := newRig(t)
	local := &fakeModel{name: "ollama", reply: `{"notes": []}`}
	rg.models["ollama"] = local
	rg.sess.ActiveProvider, rg.sess.ActiveModel = "ollama", "llama3.2:3b"
	rg.exchange("ollama", "qwen2.5-coder:3b", "asked of qwen", "answered by qwen")
	rg.exchange("ollama", "llama3.2:3b", "asked of llama", "answered by llama")

	if _, err := rg.runner(notes.AtSwitch).BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	// One provider, so one pass, on the dedicated model.
	if local.calls != 1 || local.model != "llama3.1:8b" {
		t.Errorf("%d calls on %q, want one on llama3.1:8b", local.calls, local.model)
	}
}

// reversed returns messages newest first, as a careless query might.
type reversed struct{ *mock.MessagesRepo }

func (r reversed) ListBySession(ctx context.Context, sessionID string) ([]session.Message, error) {
	msgs, err := r.MessagesRepo.ListBySession(ctx, sessionID)
	slices.Reverse(msgs)
	return msgs, err
}

func TestUnorderedStorageIsReadInOrder(t *testing.T) {
	rg := newRig(t)
	rg.lister = reversed{rg.msgs}
	rg.exchange("anthropic", opus, "a question", "an answer")

	if _, err := rg.runner(notes.Continuous).AfterReply(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if runs := rg.runs(); len(runs) != 1 || runs[0].ThroughSequenceNum != 1 {
		t.Errorf("runs = %+v, want the pass to end on the last message", runs)
	}
	if p := rg.model("anthropic").prompts[0]; strings.Index(p, "a question") > strings.Index(p, "an answer") {
		t.Errorf("the question was put after its answer:\n%s", p)
	}
}

func TestAFailedStretchStopsTheCatchUp(t *testing.T) {
	rg := newRig(t)
	local := &fakeModel{name: "ollama", reply: `{"notes": []}`}
	rg.models["ollama"] = local
	rg.model("anthropic").err = errors.New("overloaded")
	rg.exchange("anthropic", opus, "asked of Opus", "answered by Opus")
	rg.exchange("ollama", "qwen2.5-coder:3b", "asked of qwen", "answered by qwen")

	if _, err := rg.runner(notes.AtSwitch).BeforeSwitch(bg, rg.sess); err == nil {
		t.Fatal("want the failure reported")
	}
	// Reading on would leave the Opus turns behind the furthest point read.
	if local.calls != 0 {
		t.Error("read past a stretch that failed")
	}
}

// slowModel holds every call until released.
type slowModel struct {
	*fakeModel
	entered chan struct{}
	release chan struct{}
}

func (s *slowModel) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	s.entered <- struct{}{}
	<-s.release
	return s.fakeModel.SendMessage(ctx, model, history)
}

func TestPassesNeverOverlap(t *testing.T) {
	rg := newRig(t)
	slow := &slowModel{fakeModel: &fakeModel{reply: `{"notes": []}`}, entered: make(chan struct{}, 2), release: make(chan struct{})}
	rg.models["anthropic"] = slow
	rg.exchange("anthropic", opus, "a question", "an answer")
	r := rg.runner(notes.Continuous)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.AfterReply(bg, rg.sess)
		}()
	}
	<-slow.entered
	select {
	case <-slow.entered:
		t.Error("two passes read the same turns at once")
	case <-time.After(100 * time.Millisecond):
	}
	close(slow.release)
	wg.Wait()
	if slow.calls != 1 {
		t.Errorf("%d calls for one exchange, want 1", slow.calls)
	}
}

func TestNoAdapterCostsNothing(t *testing.T) {
	rg := newRig(t)
	delete(rg.models, "anthropic")
	rg.exchange("anthropic", opus, "a question", "an answer")

	if _, err := rg.runner(notes.Continuous).AfterReply(bg, rg.sess); err == nil || !strings.Contains(err.Error(), "anthropic") {
		t.Fatalf("err = %v, want one naming the provider", err)
	}
	if runs := rg.runs(); len(runs) != 0 {
		t.Errorf("runs = %+v: no model was called, so nothing was spent", runs)
	}
}

// failingNotes stores nothing.
type failingNotes struct{ *mock.NotesRepo }

func (failingNotes) Create(ctx context.Context, n *session.Note) error {
	return errors.New("disk full")
}

func TestAStorageFailureLeavesTheTurnsUnread(t *testing.T) {
	rg := newRig(t)
	m := rg.model("anthropic")
	m.reply = `{"notes": [{"kind": "fact", "content": "Go 1.22"}]}`
	rg.exchange("anthropic", opus, "Which Go?", "Go 1.22.")
	rg.store = failingNotes{rg.notes}

	if _, err := rg.runner(notes.Continuous).AfterReply(bg, rg.sess); err == nil {
		t.Fatal("want the storage failure reported")
	}
	if runs := rg.runs(); len(runs) != 1 || runs[0].Err == "" {
		t.Fatalf("runs = %+v, want one failed run", runs)
	}

	rg.store = rg.notes
	if _, err := rg.runner(notes.Continuous).BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}
	if m.calls != 2 || len(rg.stored()) != 1 {
		t.Errorf("%d calls and %d notes, want the turns read again and the note stored", m.calls, len(rg.stored()))
	}
}

func TestSpentIncludesFailedPasses(t *testing.T) {
	rg := newRig(t)
	r, m := rg.runner(notes.Continuous), rg.model("anthropic")
	m.err = errors.New("overloaded")
	rg.exchange("anthropic", opus, "a question", "an answer")
	_, _ = r.AfterReply(bg, rg.sess)
	m.err = nil
	if _, err := r.BeforeSwitch(bg, rg.sess); err != nil {
		t.Fatal(err)
	}

	runs := rg.runs()
	if len(runs) != 2 || runs[0].Err == "" {
		t.Fatalf("runs = %+v, want a failed run then a completed one", runs)
	}
	in, out, err := r.Spent(bg, rg.sess.ID)
	wantIn := runs[0].InputTokens + runs[1].InputTokens
	wantOut := runs[0].OutputTokens + runs[1].OutputTokens
	if err != nil || in != wantIn || out != wantOut || runs[0].InputTokens == 0 {
		t.Errorf("Spent = %d in / %d out, %v; want %d / %d, the failed pass included", in, out, err, wantIn, wantOut)
	}
}

// strictLedger refuses to write under a cancelled context, as a real
// database driver does.
type strictLedger struct{ *mock.ExtractionRunsRepo }

func (l strictLedger) Create(ctx context.Context, r *session.ExtractionRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.ExtractionRunsRepo.Create(ctx, r)
}

func TestACancelledPassIsStillRecorded(t *testing.T) {
	rg := newRig(t)
	rg.ledger = strictLedger{mock.NewExtractionRunsRepo()}
	rg.exchange("anthropic", opus, "a question", "an answer")

	// The user quits just as the reply arrives: the call was paid for.
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, _ = rg.runner(notes.Continuous).AfterReply(ctx, rg.sess)
	if runs := rg.runs(); len(runs) != 1 || runs[0].InputTokens == 0 {
		t.Errorf("runs = %+v, want the paid-for pass recorded", runs)
	}
}

func TestANilSessionIsAnError(t *testing.T) {
	if _, err := newRig(t).runner(notes.Continuous).BeforeSwitch(bg, nil); err == nil {
		t.Error("want an error for a nil session")
	}
}
