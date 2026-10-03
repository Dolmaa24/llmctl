package notes_test

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DhairyaP4/llmctl/internal/costestimate"
	"github.com/DhairyaP4/llmctl/internal/notes"
	"github.com/DhairyaP4/llmctl/internal/provider"
	"github.com/DhairyaP4/llmctl/internal/session"
)

// Any provider adapter can do the extracting.
var _ notes.Sender = provider.Adapter(nil)

const (
	opus  = "claude-opus-5-5"
	haiku = "claude-haiku-4-5-20251001"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

var counter costestimate.Heuristic

// fakeModel replies with canned text and records what it was asked.
type fakeModel struct {
	name    string // "anthropic" if empty
	reply   string
	err     error
	calls   int
	model   string
	history []session.Message
	prompts []string // the material of every call, in order
}

func (f *fakeModel) Name() string {
	if f.name == "" {
		return "anthropic"
	}
	return f.name
}

func (f *fakeModel) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	f.calls++
	f.model, f.history = model, history
	if len(history) > 1 {
		f.prompts = append(f.prompts, history[1].Content)
	}
	if f.err != nil {
		return session.Message{}, f.err
	}
	return session.Message{Role: session.RoleAssistant, Content: f.reply, Provider: f.Name(), Model: model}, nil
}

// prompt is the material the model was given: the notes and the messages.
func (f *fakeModel) prompt() string {
	if len(f.history) < 2 {
		return ""
	}
	return f.history[1].Content
}

func msg(seq int, role session.Role, provider, model, content string) session.Message {
	return session.Message{SessionID: "s1", SequenceNum: seq, Role: role, Provider: provider, Model: model,
		Content: content, CreatedAt: t0.Add(time.Duration(seq) * time.Minute)}
}

func user(seq int, content string) session.Message {
	return msg(seq, session.RoleUser, "", "", content)
}

// exchange is one question answered on Opus.
func exchange() []session.Message {
	return []session.Message{
		user(1, "The limiter in internal/ratelimit/bucket.go drops refills. It must stay CGO-free."),
		msg(2, session.RoleAssistant, "anthropic", opus, "Probably a data race on tokens. Run go test -race to confirm."),
	}
}

func note(id, kind, content string, minute int) session.Note {
	return session.Note{ID: id, SessionID: "s1", Content: content, Tags: []string{kind},
		Provider: "anthropic", Model: opus, CreatedAt: t0.Add(time.Duration(minute) * time.Minute)}
}

func extract(t *testing.T, reply string, recent []session.Message, existing []session.Note) (notes.Pass, *fakeModel) {
	t.Helper()
	m := &fakeModel{reply: reply}
	pass, err := notes.NewModelExtractor(m, haiku, counter).ExtractPass(context.Background(), recent, existing)
	if err != nil {
		t.Fatalf("ExtractPass: %v", err)
	}
	return pass, m
}

func find(t *testing.T, pass notes.Pass, content string) session.Note {
	t.Helper()
	for _, n := range pass.Notes {
		if n.Content == content {
			return n
		}
	}
	t.Fatalf("no note %q among %d notes", content, len(pass.Notes))
	return session.Note{}
}

func TestRunsOnTheCheapModelButAttributesTheConversation(t *testing.T) {
	pass, m := extract(t, `{"notes": [{"kind": "constraint", "content": "The binary must stay CGO-free", "source": "M1"}]}`, exchange(), nil)

	if m.model != haiku {
		t.Fatalf("extraction ran on %q, want the cheap model %q", m.model, haiku)
	}
	if pass.Usage.Provider != "anthropic" || pass.Usage.Model != haiku {
		t.Errorf("usage charged to %s %s, want the extraction model", pass.Usage.Provider, pass.Usage.Model)
	}
	// The note records where the fact came from, not which model wrote it.
	n := find(t, pass, "The binary must stay CGO-free")
	if n.Provider != "anthropic" || n.Model != opus {
		t.Errorf("note attributed to %s %s, want the conversation's %s", n.Provider, n.Model, opus)
	}
	if n.SessionID != "s1" || n.ID == "" || n.CreatedAt.IsZero() {
		t.Errorf("note not ready to store: %+v", n)
	}
}

func TestCheapTargetStaysOnTheConversationsProvider(t *testing.T) {
	cases := []struct {
		name         string
		conversation notes.Target
		want         notes.Target
	}{
		{"anthropic", notes.Target{Provider: "anthropic", Model: opus}, notes.Target{Provider: "anthropic", Model: haiku}},
		{"openrouter", notes.Target{Provider: "openrouter", Model: "openai/gpt-5"}, notes.Target{Provider: "openrouter", Model: "anthropic/claude-haiku-4.5"}},
		// Local: uses the dedicated extraction model.
		{"ollama", notes.Target{Provider: "ollama", Model: "qwen2.5-coder:3b"}, notes.Target{Provider: "ollama", Model: "llama3.1:8b"}},
		{"unlisted provider", notes.Target{Provider: "mistral", Model: "mistral-large"}, notes.Target{Provider: "mistral", Model: "mistral-large"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := notes.CheapTarget(c.conversation); got != c.want {
				t.Errorf("CheapTarget(%+v) = %+v, want %+v", c.conversation, got, c.want)
			}
		})
	}
}

func TestUserMessagesBelongToWhoeverAnsweredThem(t *testing.T) {
	// The user switched to a local model between M1 and M2.
	recent := []session.Message{
		msg(1, session.RoleAssistant, "anthropic", opus, "Use a compare-and-swap loop."),
		user(2, "Agreed. Never use Redis for this."),
		msg(3, session.RoleAssistant, "ollama", "qwen2.5-coder:3b", "Understood, in-process only."),
	}
	pass, _ := extract(t, `{"notes": [
		{"kind": "decision", "content": "Refill with a CAS loop", "source": "M1"},
		{"kind": "constraint", "content": "Never use Redis", "source": "M2"},
		{"kind": "fact", "content": "Source not given"}]}`, recent, nil)

	for content, want := range map[string]string{
		"Refill with a CAS loop": opus,               // the reply's own author
		"Never use Redis":        "qwen2.5-coder:3b", // said to the model that answered, after the switch
		"Source not given":       "qwen2.5-coder:3b", // the latest message stands in
	} {
		if got := find(t, pass, content).Model; got != want {
			t.Errorf("%q attributed to %s, want %s", content, got, want)
		}
	}
}

func TestHandlesAreReadTolerantly(t *testing.T) {
	recent := []session.Message{
		msg(1, session.RoleAssistant, "anthropic", opus, "Suspect the ticker."),
		user(2, "It was the lock."),
		msg(3, session.RoleAssistant, "ollama", "qwen2.5-coder:3b", "Then use CAS."),
	}
	suspect := note("n-ticker", "question", "Is the ticker at fault?", 0)
	// A number for a message, and a list where one note was asked for.
	pass, _ := extract(t, `{"notes": [{"kind": "fact", "content": "The lock was at fault", "source": 1, "replaces": ["N1", "N2"]}]}`,
		recent, []session.Note{suspect})

	n := find(t, pass, "The lock was at fault")
	if n.Model != opus {
		t.Errorf("source 1 attributed to %s, want M1's %s", n.Model, opus)
	}
	if pass.Supersedes[suspect.ID] != n.ID {
		t.Errorf("Supersedes = %v, want the first listed note replaced", pass.Supersedes)
	}
}

func TestOverturnedNoteIsReportedAndLinked(t *testing.T) {
	race := note("n-race", "question", "Suspect a data race on tokens", 0)
	pass, _ := extract(t, `{"notes": [{"kind": "fact", "content": "-race is clean: it is lock contention", "replaces": "N1"}]}`,
		exchange(), []session.Note{race})

	fix := find(t, pass, "-race is clean: it is lock contention")
	if pass.Supersedes[race.ID] != fix.ID {
		t.Fatalf("Supersedes = %v, want %s replaced by %s", pass.Supersedes, race.ID, fix.ID)
	}
	if fix.LinksTo == nil || *fix.LinksTo != race.ID {
		t.Errorf("the replacement should link back to %s, got %v", race.ID, fix.LinksTo)
	}
}

func TestLinksResolveToNoteIDs(t *testing.T) {
	existing := []session.Note{note("n-go", "fact", "Go 1.22", 0), note("n-cgo", "constraint", "Stay CGO-free", 1)}
	pass, _ := extract(t, `{"notes": [{"kind": "fact", "content": "modernc.org/sqlite keeps the build CGO-free", "links_to": "N2"}]}`,
		exchange(), existing)

	if n := find(t, pass, "modernc.org/sqlite keeps the build CGO-free"); n.LinksTo == nil || *n.LinksTo != "n-cgo" {
		t.Errorf("LinksTo = %v, want n-cgo", n.LinksTo)
	}
	if len(pass.Supersedes) != 0 {
		t.Errorf("a link is not a replacement: %v", pass.Supersedes)
	}
}

func TestAReplacementMustBeOfTheSameKind(t *testing.T) {
	cases := []struct {
		prev, next string
		allowed    bool
	}{
		{"goal", "constraint", false}, // seen from llama3.2:3b: the goal would have vanished
		{"decision", "fact", false},
		{"decision", "decision", true},
		{"fact", "fact", true},
		{"question", "fact", true}, // answering a question closes it
		{"question", "decision", true},
		{"hypothesis", "fact", true}, // a kind this extractor does not make: nothing to check
	}
	for _, c := range cases {
		t.Run(c.prev+" by "+c.next, func(t *testing.T) {
			old := note("n-old", c.prev, "The earlier note", 0)
			pass, _ := extract(t, `{"notes": [{"kind": "`+c.next+`", "content": "The later note", "replaces": "N1"}]}`,
				exchange(), []session.Note{old})

			_, replaced := pass.Supersedes[old.ID]
			if replaced != c.allowed {
				t.Errorf("replaced = %v, want %v", replaced, c.allowed)
			}
			// Refused or not, the model judged the two related.
			if n := find(t, pass, "The later note"); n.LinksTo == nil || *n.LinksTo != old.ID {
				t.Errorf("LinksTo = %v, want %s", n.LinksTo, old.ID)
			}
		})
	}
}

func TestInventedReferencesAreDropped(t *testing.T) {
	existing := []session.Note{note("n-1", "fact", "Go 1.22", 0)}
	pass, _ := extract(t, `{"notes": [
		{"kind": "fact", "content": "Replaces a note that does not exist", "replaces": "N7"},
		{"kind": "fact", "content": "Links to a message, not a note", "links_to": "M1"},
		{"kind": "fact", "content": "Cites a message that does not exist", "source": "M9"}]}`, exchange(), existing)

	if len(pass.Supersedes) != 0 {
		t.Errorf("Supersedes = %v, want nothing replaced", pass.Supersedes)
	}
	for _, n := range pass.Notes {
		if n.LinksTo != nil {
			t.Errorf("%q links to %s, a reference the model invented", n.Content, *n.LinksTo)
		}
	}
	if got := find(t, pass, "Cites a message that does not exist").Model; got != opus {
		t.Errorf("unknown source attributed to %s, want the latest message's %s", got, opus)
	}
}

func TestSupersededNotesAreNeitherShownNorReplaceable(t *testing.T) {
	newer := "n-newer"
	stale := note("n-stale", "decision", "Guard the whole refill block with a mutex", 0)
	stale.SupersededBy = &newer
	current := note(newer, "decision", "Refill with atomic CAS", 1)

	pass, m := extract(t, `{"notes": [
		{"kind": "decision", "content": "Keep the mutex for config reloads only", "replaces": "N1"},
		{"kind": "decision", "content": "Aimed at the stale note", "replaces": "N2"}]}`,
		exchange(), []session.Note{stale, current})

	if strings.Contains(m.prompt(), stale.Content) {
		t.Error("a superseded note was shown to the model")
	}
	// N1 is the only active note, so it is the current one.
	want := map[string]string{current.ID: find(t, pass, "Keep the mutex for config reloads only").ID}
	if len(pass.Supersedes) != 1 || pass.Supersedes[current.ID] != want[current.ID] {
		t.Errorf("Supersedes = %v, want %v", pass.Supersedes, want)
	}
}

func TestRepeatedNotesAreNotStoredTwice(t *testing.T) {
	existing := []session.Note{note("n-1", "decision", "Use atomic CAS in refill()", 0)}
	pass, _ := extract(t, `{"notes": [
		{"kind": "decision", "content": "use atomic  CAS in refill()."},
		{"kind": "fact", "content": "BenchmarkRefillUnderLoad checks the drop rate"},
		{"kind": "fact", "content": "BenchmarkRefillUnderLoad checks the drop rate"}]}`, exchange(), existing)

	if len(pass.Notes) != 1 || pass.Notes[0].Content != "BenchmarkRefillUnderLoad checks the drop rate" {
		t.Errorf("got %d notes, want only the one new fact once: %+v", len(pass.Notes), pass.Notes)
	}
}

func TestNotesCopiedFromThePromptAreDropped(t *testing.T) {
	pass, _ := extract(t, `{"notes": [
		{"kind": "<kind>", "content": "<the note>", "source": "<message id>"},
		{"kind": "goal", "content": "goal"},
		{"kind": "fact", "content": "The limiter lives in internal/ratelimit/bucket.go"}]}`, exchange(), nil)

	if len(pass.Notes) != 1 {
		t.Errorf("got %d notes, want only the real one: %+v", len(pass.Notes), pass.Notes)
	}
}

func TestRepliesAreReadTolerantly(t *testing.T) {
	cases := map[string]string{
		"plain":             `{"notes": [{"kind": "fact", "content": "Go 1.22"}]}`,
		"fenced":            "```json\n{\"notes\": [{\"kind\": \"fact\", \"content\": \"Go 1.22\"}]}\n```",
		"wrapped in prose":  "Here are the notes [as asked]:\n{\"notes\": [{\"kind\": \"fact\", \"content\": \"Go 1.22\"}]}\nHope that helps!",
		"bare array":        `[{"kind": "fact", "content": "Go 1.22"}]`,
		"single object":     `{"kind": "fact", "content": "Go 1.22"}`,
		"strings in a list": `{"notes": ["Go 1.22"]}`,
		"numbers for ids":   `{"notes": [{"kind": "fact", "content": "Go 1.22", "source": 2, "replaces": 0, "links_to": null}]}`,
		"odd field types":   `{"notes": [{"kind": 7, "content": "Go 1.22", "topics": "go, versions", "source": ["M2", "M1"]}]}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			pass, _ := extract(t, reply, exchange(), nil)
			if got := find(t, pass, "Go 1.22").Tags[0]; got != "fact" {
				t.Errorf("kind = %q, want fact", got)
			}
		})
	}
}

func TestUnreadableReplyFailsButIsStillCharged(t *testing.T) {
	m := &fakeModel{reply: "Sorry, I can't take notes on that."}
	pass, err := notes.NewModelExtractor(m, haiku, counter).ExtractPass(context.Background(), exchange(), nil)
	if err == nil || !strings.Contains(err.Error(), haiku) {
		t.Fatalf("err = %v, want a failure naming %s", err, haiku)
	}
	if len(pass.Notes) != 0 {
		t.Errorf("got notes from an unreadable reply: %+v", pass.Notes)
	}
	if pass.Usage.InputTokens == 0 || pass.Usage.OutputTokens == 0 {
		t.Errorf("usage = %+v: a failed pass was still paid for", pass.Usage)
	}
}

func TestNothingNewIsNotAnError(t *testing.T) {
	for _, reply := range []string{`{"notes": []}`, `{"notes": null}`, `[]`} {
		pass, _ := extract(t, reply, exchange(), nil)
		if len(pass.Notes) != 0 || pass.Usage.InputTokens == 0 {
			t.Errorf("%s: got %d notes and usage %+v, want none and a charged pass", reply, len(pass.Notes), pass.Usage)
		}
	}
}

func TestAnEmptyWindowIsNeverSent(t *testing.T) {
	pass, m := extract(t, `{"notes": []}`, nil, []session.Note{note("n-1", "fact", "Go 1.22", 0)})
	if m.calls != 0 {
		t.Errorf("called the model %d times with nothing new to read", m.calls)
	}
	if pass.Usage != (notes.Usage{}) {
		t.Errorf("usage = %+v, want nothing spent", pass.Usage)
	}
}

func TestAFailedCallIsWrappedAndCharged(t *testing.T) {
	overloaded := errors.New("overloaded")
	m := &fakeModel{err: overloaded}
	pass, err := notes.NewModelExtractor(m, haiku, counter).ExtractPass(context.Background(), exchange(), nil)
	if !errors.Is(err, overloaded) {
		t.Fatalf("err = %v, want it to wrap %v", err, overloaded)
	}
	if pass.Usage.InputTokens == 0 {
		t.Error("a failed call should still be charged for its input")
	}
}

func TestUsageIsCountedWithTheSharedCounter(t *testing.T) {
	reply := `{"notes": [{"kind": "fact", "content": "Go 1.22"}]}`
	pass, m := extract(t, reply, exchange(), nil)

	wantIn := counter.CountMessages(m.history)
	wantOut := counter.CountMessages([]session.Message{{Role: session.RoleAssistant, Content: reply}})
	if pass.Usage.InputTokens != wantIn || pass.Usage.OutputTokens != wantOut {
		t.Errorf("usage = %d in / %d out, want %d / %d", pass.Usage.InputTokens, pass.Usage.OutputTokens, wantIn, wantOut)
	}
	if pass.Usage.PromptVersion != notes.PromptVersion {
		t.Errorf("prompt version = %q, want %q", pass.Usage.PromptVersion, notes.PromptVersion)
	}
}

func TestTagsPutTheKindFirst(t *testing.T) {
	pass, _ := extract(t, `{"notes": [
		{"kind": "Decision", "content": "Use CAS", "topics": ["Concurrency", "concurrency", "fact", "#go", "lock free", "extra"]},
		{"kind": "fact", "content": "Topics given as one string", "topics": "Go, SQLite"},
		{"kind": "hypothesis", "content": "Maybe the ticker drifts"}]}`, exchange(), nil)

	want := []string{"decision", "concurrency", "go", "lock-free"}
	if got := find(t, pass, "Use CAS").Tags; !slices.Equal(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
	if got := find(t, pass, "Topics given as one string").Tags; !slices.Equal(got, []string{"fact", "go", "sqlite"}) {
		t.Errorf("tags = %v, want [fact go sqlite]", got)
	}
	if got := find(t, pass, "Maybe the ticker drifts").Tags; !slices.Equal(got, []string{"fact"}) {
		t.Errorf("an unknown kind should become fact, got %v", got)
	}
}

func TestNotesKeepTheModelsOrder(t *testing.T) {
	pass, _ := extract(t, `{"notes": [
		{"kind": "goal", "content": "alpha"}, {"kind": "fact", "content": "bravo"}, {"kind": "fact", "content": "charlie"},
		{"kind": "fact", "content": "delta"}, {"kind": "fact", "content": "foxtrot"}]}`, exchange(), nil)

	// Notes from one pass share a timestamp, so storage and the handoff fall
	// back to the ID to order them.
	got := slices.Clone(pass.Notes)
	sort.SliceStable(got, func(i, j int) bool {
		if !got[i].CreatedAt.Equal(got[j].CreatedAt) {
			return got[i].CreatedAt.Before(got[j].CreatedAt)
		}
		return got[i].ID < got[j].ID
	})
	var order []string
	for _, n := range got {
		order = append(order, n.Content)
	}
	if want := []string{"alpha", "bravo", "charlie", "delta", "foxtrot"}; !slices.Equal(order, want) {
		t.Errorf("sorted as storage would: %v, want %v", order, want)
	}
}

func TestMessagesAreNumberedInConversationOrder(t *testing.T) {
	recent := exchange()
	slices.Reverse(recent)
	_, m := extract(t, `{"notes": []}`, recent, nil)

	p := m.prompt()
	first, second := strings.Index(p, `id="M1" role="user"`), strings.Index(p, `id="M2" role="assistant"`)
	if first < 0 || second < first {
		t.Errorf("messages not numbered in sequence order:\n%s", p)
	}
}

func TestAWindowSpanningSessionsIsRefused(t *testing.T) {
	recent := exchange()
	recent[1].SessionID = "s2"
	m := &fakeModel{reply: `{"notes": []}`}
	if _, err := notes.NewModelExtractor(m, haiku, counter).ExtractPass(context.Background(), recent, nil); err == nil {
		t.Fatal("want an error for turns from two sessions")
	}
	if m.calls != 0 {
		t.Error("the model was called anyway")
	}
}

func TestExtractReturnsThePassNotes(t *testing.T) {
	m := &fakeModel{reply: `{"notes": [{"kind": "fact", "content": "Go 1.22"}]}`}
	var x notes.Extractor = notes.NewModelExtractor(m, haiku, counter)
	got, err := x.Extract(context.Background(), exchange(), nil)
	if err != nil || len(got) != 1 || got[0].Content != "Go 1.22" {
		t.Errorf("Extract = %+v, %v", got, err)
	}
}
