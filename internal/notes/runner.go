package notes

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/DhairyaP4/llmctl/internal/session"
)

// Schedule is when extraction runs, which is PRD open question #1. It counts
// the replies that may wait before a background pass takes notes on them.
//
// Continuous takes notes after every reply, so a switch finds them ready,
// but it pays for a pass on every exchange whether or not the user ever
// switches, and every pass re-sends the instructions and the existing notes.
// AtSwitch pays nothing until a switch, then makes the user wait while one
// pass reads everything since the last. Values in between batch the
// background passes, trading how current the notes are for fewer passes.
// The evaluation settles which is best by running them (plan TASK-022).
type Schedule int

const (
	AtSwitch   Schedule = 0
	Continuous Schedule = 1
)

// NoteStore is satisfied by storage.NotesRepo.
type NoteStore interface {
	Create(ctx context.Context, n *session.Note) error
	ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]session.Note, error)
	MarkSuperseded(ctx context.Context, noteID, supersededByID string) error
}

// Ledger keeps the record of extraction runs: the token ledger of plan
// TASK-020, and the record of how far extraction has read.
//
// Nothing in storage satisfies it yet. That needs an extraction_runs table,
// an interface change for the storage module to approve. Until then
// mock.Ledger keeps runs in memory, and a restart forgets them, so the first
// pass after a restart reads the whole transcript again.
// Ledger is satisfied by storage.ExtractionRunsRepo.
type Ledger interface {
	Create(ctx context.Context, r *session.ExtractionRun) error
	ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error)
}

// SenderLookup returns the adapter for a provider. The app wraps the
// provider registry's Get.
type SenderLookup func(provider string) (Sender, error)

// Result is what one call to a Runner did: a run for each pass, in order,
// and every note stored. Both are empty when there was nothing to read.
type Result struct {
	Runs  []session.ExtractionRun
	Notes []session.Note
}

// Runner decides when extraction runs, keeps each pass to the turns since
// the last, and stores what the passes produce.
//
// Its methods block for as long as a model call takes, so the app calls them
// from a background command, never from Update (NFR-1). The app, rather than
// the handoff builder, calls BeforeSwitch: session cannot import notes, so
// the builder cannot trigger extraction itself as the contract suggests.
type Runner struct {
	schedule Schedule
	messages session.MessageLister
	notes    NoteStore
	ledger   Ledger
	senders  SenderLookup
	counter  session.TokenCounter

	// mu keeps passes from overlapping. Two passes reading from the same
	// point would take the same notes twice, and be paid for twice.
	mu sync.Mutex
}

func NewRunner(schedule Schedule, messages session.MessageLister, notes NoteStore, ledger Ledger, senders SenderLookup, counter session.TokenCounter) *Runner {
	return &Runner{schedule: schedule, messages: messages, notes: notes, ledger: ledger, senders: senders, counter: counter}
}

// AfterReply is called once a reply has been stored. It takes notes on
// everything unread once the schedule's number of replies is waiting. Under
// AtSwitch it returns at once, without touching storage.
func (r *Runner) AfterReply(ctx context.Context, sess *session.Session) (Result, error) {
	if r.schedule <= AtSwitch {
		return Result{}, nil
	}
	return r.catchUp(ctx, sess, int(r.schedule))
}

// BeforeSwitch takes notes on everything unread, under any schedule. Call it
// when the user asks to switch, before building the plan the confirmation
// shows, so the plan is priced with the notes that will actually be sent.
// Under Continuous there is usually nothing left to read, unless a
// background pass failed, or is still running, in which case this waits.
func (r *Runner) BeforeSwitch(ctx context.Context, sess *session.Session) (Result, error) {
	return r.catchUp(ctx, sess, 0)
}

// Spent returns the tokens extraction has cost a session so far, failed
// passes included: the extraction side of C_total that the switch screen
// shows beside the handoff's saving (plan REQ-002).
func (r *Runner) Spent(ctx context.Context, sessionID string) (input, output int, err error) {
	runs, err := r.ledger.ListBySession(ctx, sessionID)
	if err != nil {
		return 0, 0, fmt.Errorf("notes: loading extraction runs: %w", err)
	}
	for _, run := range runs {
		input += run.InputTokens
		output += run.OutputTokens
	}
	return input, output, nil
}

// catchUp reads every unread message, once at least minReplies replies are
// among them, in one pass per provider.
func (r *Runner) catchUp(ctx context.Context, sess *session.Session, minReplies int) (Result, error) {
	if sess == nil {
		return Result{}, errors.New("notes: cannot take notes for a nil session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	runs, err := r.ledger.ListBySession(ctx, sess.ID)
	if err != nil {
		return Result{}, fmt.Errorf("notes: loading extraction runs: %w", err)
	}
	msgs, err := r.messages.ListBySession(ctx, sess.ID)
	if err != nil {
		return Result{}, fmt.Errorf("notes: loading messages: %w", err)
	}
	pending := unread(msgs, runs)
	if len(pending) == 0 || replies(pending) < minReplies {
		return Result{}, nil
	}

	var res Result
	for _, seg := range segments(pending, sess) {
		run, stored, err := r.pass(ctx, sess.ID, seg)
		if run != nil {
			res.Runs = append(res.Runs, *run)
		}
		res.Notes = append(res.Notes, stored...)
		// Runs must read the conversation in order, so a failure stops
		// here. Later stretches stay unread for the next call.
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// pass takes notes on one stretch on the cheap model for its provider,
// stores them, and records the run. It returns no run when it failed before
// calling the model, since nothing was spent.
func (r *Runner) pass(ctx context.Context, sessionID string, seg segment) (*session.ExtractionRun, []session.Note, error) {
	target := CheapTarget(seg.conversation)
	sender, err := r.senders(target.Provider)
	if err != nil {
		return nil, nil, fmt.Errorf("notes: no adapter to extract with on %s: %w", target.Provider, err)
	}
	existing, err := r.notes.ListBySession(ctx, sessionID, false)
	if err != nil {
		return nil, nil, fmt.Errorf("notes: loading notes: %w", err)
	}

	p, err := NewModelExtractor(sender, target.Model, r.counter).ExtractPass(ctx, seg.msgs, existing)
	var stored []session.Note
	if err == nil {
		stored, _, err = r.store(ctx, p)
	}

	run := &session.ExtractionRun{
		SessionID:          sessionID,
		ThroughSequenceNum: seg.msgs[len(seg.msgs)-1].SequenceNum,
		Provider:           target.Provider,
		Model:              target.Model,
		PromptVersion:      PromptVersion,
		InputTokens:        p.Usage.InputTokens,
		OutputTokens:       p.Usage.OutputTokens,
		CreatedAt:          time.Now(),
	}
	for _, n := range stored {
		run.NoteIDs = append(run.NoteIDs, n.ID)
	}
	if err != nil {
		run.Err = err.Error()
	}
	// Recorded even if the caller has given up: a cancelled pass may
	// still have been billed, and extraction cost must not be understated.
	if lerr := r.ledger.Create(context.WithoutCancel(ctx), run); lerr != nil {
		err = errors.Join(err, fmt.Errorf("notes: recording the extraction run: %w", lerr))
	}
	return run, stored, err
}

// store saves a pass's notes and then marks what they overturned. The order
// matters: superseded_by references notes(id), so a replacement must exist
// before anything can point to it.
//
// If storing fails partway, the notes already saved stay (NFR-6) and the
// turns are read again; duplicates of the saved notes are then dropped by
// the extractor. Storage could make a pass atomic with a transaction.
func (r *Runner) store(ctx context.Context, p Pass) (stored []session.Note, superseded int, err error) {
	for i := range p.Notes {
		if err := r.notes.Create(ctx, &p.Notes[i]); err != nil {
			return stored, superseded, fmt.Errorf("notes: storing a note: %w", err)
		}
		stored = append(stored, p.Notes[i])
	}
	for _, old := range slices.Sorted(maps.Keys(p.Supersedes)) {
		if err := r.notes.MarkSuperseded(ctx, old, p.Supersedes[old]); err != nil {
			return stored, superseded, fmt.Errorf("notes: marking a note superseded: %w", err)
		}
		superseded++
	}
	return stored, superseded, nil
}

// unread returns, in conversation order, the messages after the furthest
// point any completed run has read.
func unread(msgs []session.Message, runs []session.ExtractionRun) []session.Message {
	through, read := 0, false
	for _, run := range runs {
		if run.Err == "" && (!read || run.ThroughSequenceNum > through) {
			through, read = run.ThroughSequenceNum, true
		}
	}
	var out []session.Message
	for _, m := range msgs {
		if !read || m.SequenceNum > through {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SequenceNum < out[j].SequenceNum })
	return out
}

func replies(msgs []session.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == session.RoleAssistant {
			n++
		}
	}
	return n
}

// segment is a stretch of unread messages that were all sent to one
// provider, and the conversation they belong to.
type segment struct {
	msgs         []session.Message
	conversation Target
}

// segments splits unread messages wherever the provider changes, so that no
// pass shows one provider turns that were held on another. CheapTarget's
// promise then holds even when a failed pass or a restart leaves turns from
// both sides of a switch unread. A user's message goes with the reply that
// answered it, by the same rule that attributes notes; a message no reply
// accounts for belongs to the session's active provider.
func segments(msgs []session.Message, sess *session.Session) []segment {
	var out []segment
	for i := range msgs {
		provider, model := attribution(msgs, i)
		if provider == "" {
			provider, model = sess.ActiveProvider, sess.ActiveModel
		}
		if n := len(out); n > 0 && out[n-1].conversation.Provider == provider {
			out[n-1].msgs = append(out[n-1].msgs, msgs[i])
			out[n-1].conversation.Model = model
			continue
		}
		out = append(out, segment{msgs: []session.Message{msgs[i]}, conversation: Target{Provider: provider, Model: model}})
	}
	return out
}
