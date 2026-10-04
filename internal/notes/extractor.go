// Package notes turns recent conversation turns into structured Note records.
// It imports session for the canonical types; session never imports notes.
package notes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// Extractor produces new notes from recent conversation.
//
// Implementations may call a provider adapter internally (typically a small,
// cheap model) — that is an implementation detail, not part of the contract.
//
// Extraction must operate on the turns since the last extraction, never on the
// full transcript: re-extracting everything on each pass makes extraction cost
// grow quadratically with session length and silently exceed whatever the
// distilled handoff saves.
type Extractor interface {
	Extract(ctx context.Context, recent []session.Message, existing []session.Note) ([]session.Note, error)
}

// Sender is the part of provider.Adapter that extraction uses.
type Sender interface {
	Name() string
	SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error)
}

// Usage is what one extraction pass cost, attributed to the model that did
// the extracting rather than the one the conversation was using.
//
// Tokens are counted with the same counter as the handoff estimates, so
// extraction cost and handoff cost can be added together honestly. Pricing
// them is left to whoever reports the total, since the two usually run on
// models with different prices.
type Usage struct {
	Provider      string
	Model         string
	PromptVersion string
	InputTokens   int
	OutputTokens  int
}

// Pass is the outcome of one extraction pass.
type Pass struct {
	// Notes are the new notes, IDs already assigned.
	Notes []session.Note

	// Supersedes maps the ID of an existing note to the ID of the new note
	// that overturns it. SupersededBy lives on the old note, which Extract
	// does not return, so the pass reports it here. Whoever stores the pass
	// must create the new notes before calling NotesRepo.MarkSuperseded,
	// because superseded_by references notes(id).
	Supersedes map[string]string

	Usage Usage
}

// ModelExtractor extracts notes by asking a model, normally the cheap one
// CheapTarget chooses for the provider the conversation is on.
type ModelExtractor struct {
	sender  Sender
	model   string
	counter session.TokenCounter
}

var _ Extractor = (*ModelExtractor)(nil)

// NewModelExtractor returns an extractor that runs on the given provider's
// model, counting tokens with counter.
func NewModelExtractor(sender Sender, model string, counter session.TokenCounter) *ModelExtractor {
	return &ModelExtractor{sender: sender, model: model, counter: counter}
}

// Extract satisfies the contract. It discards what the pass overturned and
// what it cost, so anything that stores notes should call ExtractPass.
func (x *ModelExtractor) Extract(ctx context.Context, recent []session.Message, existing []session.Note) ([]session.Note, error) {
	p, err := x.ExtractPass(ctx, recent, existing)
	return p.Notes, err
}

// ExtractPass takes notes on recent, given the session's existing notes.
//
// The returned Usage is filled in whenever the model was called, including
// when the pass then fails: those tokens were spent all the same, and the
// cost of extraction includes the passes that produced nothing.
func (x *ModelExtractor) ExtractPass(ctx context.Context, recent []session.Message, existing []session.Note) (Pass, error) {
	// An empty window is never sent: the pass would pay for the
	// instructions and every existing note, and learn nothing.
	if len(recent) == 0 {
		return Pass{}, nil
	}
	for _, m := range recent {
		if m.SessionID != recent[0].SessionID {
			return Pass{}, errors.New("notes: recent turns span more than one session")
		}
	}
	msgs := append([]session.Message(nil), recent...)
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].SequenceNum < msgs[j].SequenceNum })
	active := activeNotes(existing)

	req := request(msgs, active)
	usage := Usage{
		Provider:      x.sender.Name(),
		Model:         x.model,
		PromptVersion: PromptVersion,
		InputTokens:   x.counter.CountMessages(req),
	}
	reply, err := x.sender.SendMessage(ctx, x.model, req)
	if err != nil {
		// Counted as spent even if the provider never billed it. Overstating
		// extraction cost can only make distillation look worse than it is,
		// never better.
		return Pass{Usage: usage}, fmt.Errorf("notes: extracting with %s %s: %w", usage.Provider, x.model, err)
	}
	usage.OutputTokens = x.counter.CountMessages([]session.Message{reply})

	drafts, err := parseReply(reply.Content)
	if err != nil {
		return Pass{Usage: usage}, fmt.Errorf("notes: reading the reply from %s %s: %w", usage.Provider, x.model, err)
	}
	pass, err := assemble(drafts, msgs, active)
	pass.Usage = usage
	return pass, err
}

// assemble turns drafts into notes: attributed to the conversation they came
// from, tagged, deduplicated, and with note handles resolved to real IDs.
// A reference the model invented is dropped, never guessed at.
func assemble(drafts []draft, msgs []session.Message, active []session.Note) (Pass, error) {
	now := time.Now()
	seen := make(map[string]bool, len(active)+len(drafts))
	for _, n := range active {
		seen[fold(n.Content)] = true
	}
	pass := Pass{Supersedes: map[string]string{}}
	for _, d := range drafts {
		content := strings.Join(strings.Fields(d.content), " ")
		if content == "" || seen[fold(content)] || copiedFromPrompt(content) {
			continue
		}
		seen[fold(content)] = true

		// Version 7 IDs sort in the order they were made, so notes from
		// one pass, which share a timestamp, still list in the order the
		// model wrote them.
		id, err := uuid.NewV7()
		if err != nil {
			return pass, fmt.Errorf("notes: generating a note ID: %w", err)
		}
		provider, model := attribution(msgs, d.source)
		n := session.Note{
			ID:        id.String(),
			SessionID: msgs[0].SessionID,
			Content:   content,
			Provider:  provider,
			Model:     model,
			Tags:      tags(d.kind, d.topics),
			CreatedAt: now,
		}
		// A replacement also links back to what it replaced, so the pair can
		// be followed from either end. The link stands even when the
		// replacement is refused: the model still judged them related.
		if old, ok := handle(active, d.replaces); ok {
			if canReplace(n.Tags[0], kindOf(active[d.replaces])) {
				pass.Supersedes[old] = n.ID
			}
			n.LinksTo = &old
		}
		if link, ok := handle(active, d.linksTo); ok {
			n.LinksTo = &link
		}
		pass.Notes = append(pass.Notes, n)
	}
	return pass, nil
}

// attribution returns the provider and model whose conversation message i
// belongs to. A reply belongs to whoever wrote it. A user's message belongs
// to whoever answered it: after a switch the user is talking to the new
// provider, not the old one. With no usable source, the latest message
// stands in.
//
// The extraction model never appears here. It wrote the note, but the fact
// came from the conversation, and that is what FR-3.2 attributes.
func attribution(msgs []session.Message, i int) (provider, model string) {
	if i < 0 || i >= len(msgs) {
		i = len(msgs) - 1
	}
	for j := i; j < len(msgs); j++ {
		if msgs[j].Role == session.RoleAssistant {
			return msgs[j].Provider, msgs[j].Model
		}
	}
	for j := i - 1; j >= 0; j-- {
		if msgs[j].Role == session.RoleAssistant {
			return msgs[j].Provider, msgs[j].Model
		}
	}
	return "", ""
}

// canReplace reports whether a note of kind next may overturn one of kind
// prev. A note is replaced only by one of its own kind, and a question by
// whatever answers it. Small models misjudge this: one marked the user's
// goal as overturned by an unrelated constraint, which would have dropped
// the goal from every later handoff. A wrongly kept note costs a few tokens;
// a wrongly dropped one costs the new model a fact it needed.
//
// A note whose kind is unknown, because it was made elsewhere, can be
// replaced by anything: there is nothing to check against.
func canReplace(next, prev string) bool {
	return prev == "" || next == prev || prev == "question"
}

// kindOf returns a note's kind: its first tag, if that is one of Kinds.
func kindOf(n session.Note) string {
	if len(n.Tags) > 0 && slices.Contains(Kinds, n.Tags[0]) {
		return n.Tags[0]
	}
	return ""
}

// maxTopics bounds the tags after the kind. Tags are paid for in every
// handoff that carries the note.
const maxTopics = 3

// tags puts the note's kind first, then up to maxTopics distinct topics. An
// unrecognised kind becomes "fact" rather than losing the note.
func tags(kind string, topics []string) []string {
	k := tag(kind)
	if !slices.Contains(Kinds, k) {
		k = "fact"
	}
	out := []string{k}
	for _, t := range topics {
		t = tag(t)
		if t == "" || slices.Contains(Kinds, t) || slices.Contains(out, t) {
			continue
		}
		out = append(out, t)
		if len(out) == 1+maxTopics {
			break
		}
	}
	return out
}

func tag(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimLeft(strings.TrimSpace(s), "#"))), "-")
}

// fold is the form in which two notes count as the same note: case,
// spacing and closing punctuation do not make a note new.
func fold(s string) string {
	return strings.ToLower(strings.TrimRight(strings.Join(strings.Fields(s), " "), ".!;:, "))
}

var foldedInstructions = fold(instructions)

// copiedFromPrompt reports whether a note's whole text appears in the
// instructions. Such a note was taken from the prompt, such as a placeholder
// from the reply shape, rather than from the conversation.
func copiedFromPrompt(content string) bool {
	return strings.Contains(foldedInstructions, fold(content))
}

// handle resolves a note handle's index to the ID of that active note.
func handle(active []session.Note, i int) (string, bool) {
	if i < 0 || i >= len(active) {
		return "", false
	}
	return active[i].ID, true
}

// activeNotes drops superseded notes and orders the rest chronologically, as
// the handoff does. Only active notes are shown to the model, so a note that
// has already been overturned cannot be overturned again.
func activeNotes(notes []session.Note) []session.Note {
	var out []session.Note
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
