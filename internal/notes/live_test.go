//go:build live

// The live check runs the real extraction prompt against real models, so the
// prompt is tested against what models actually return rather than against
// replies written by hand. It never runs in CI.
//
// Local models on Ollama, free:
//
//	go test -tags live -run Live -v ./internal/notes
//	LIVE_MODELS=qwen2.5:3b go test -tags live -run Live -v ./internal/notes
//
// The default cheap model, Claude Haiku 4.5, with your own key from your own
// shell (a run costs well under a cent):
//
//	LIVE_PROVIDER=anthropic go test -tags live -run Live -v ./internal/notes
//
// LIVE_MODE=switch takes all the notes in one pass at the end, as
// switch-triggered extraction would, instead of one pass per exchange.
package notes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/costestimate"
	"github.com/Dolmaa24/llmctl/internal/session"
)

// liveSender is a Sender that also keeps the raw reply, and the provider's
// own token count where it gives one, for calibrating the heuristic.
type liveSender interface {
	Sender
	last() (reply string, in, out int)
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func post(ctx context.Context, url string, headers map[string]string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s: %s", resp.Status, e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ollama and anthropic are minimal Senders for the live check only. The real
// adapters are the provider module's to write.
type ollama struct{ reply string }

func (o *ollama) Name() string                      { return "ollama" }
func (o *ollama) last() (reply string, in, out int) { return o.reply, 0, 0 }

func (o *ollama) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	var msgs []chatMsg
	for _, m := range history {
		msgs = append(msgs, chatMsg{string(m.Role), m.Content})
	}
	body := map[string]any{"model": model, "stream": false, "messages": msgs, "options": map[string]any{"temperature": 0}}
	var out struct {
		Message chatMsg `json:"message"`
	}
	if err := post(ctx, "http://localhost:11434/api/chat", nil, body, &out); err != nil {
		return session.Message{}, err
	}
	o.reply = out.Message.Content
	return session.Message{Role: session.RoleAssistant, Content: o.reply, Provider: "ollama", Model: model}, nil
}

type anthropic struct {
	key     string
	reply   string
	in, out int
}

func (a *anthropic) Name() string                      { return "anthropic" }
func (a *anthropic) last() (reply string, in, out int) { return a.reply, a.in, a.out }

func (a *anthropic) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	var system string
	var msgs []chatMsg
	for _, m := range history {
		if m.Role == session.RoleSystem {
			system += m.Content
			continue
		}
		msgs = append(msgs, chatMsg{string(m.Role), m.Content})
	}
	body := map[string]any{"model": model, "max_tokens": 2048, "temperature": 0, "system": system, "messages": msgs}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	headers := map[string]string{"x-api-key": a.key, "anthropic-version": "2023-06-01"}
	if err := post(ctx, "https://api.anthropic.com/v1/messages", headers, body, &out); err != nil {
		return session.Message{}, err
	}
	a.reply = ""
	for _, c := range out.Content {
		a.reply += c.Text
	}
	a.in, a.out = out.Usage.InputTokens, out.Usage.OutputTokens
	return session.Message{Role: session.RoleAssistant, Content: a.reply, Provider: "anthropic", Model: model}, nil
}

// liveConversation plants what the handoff must carry: a goal, a
// measurement, a constraint, a suspicion that is later ruled out, a decision,
// a prohibition, and identifiers. The user switches provider before M5.
func liveConversation() []session.Message {
	turn := func(seq int, role session.Role, provider, model, content string) session.Message {
		return session.Message{SessionID: "live", SequenceNum: seq, Role: role, Provider: provider, Model: model, Content: content}
	}
	const opus, qwen = "claude-opus-5-5", "qwen2.5-coder:3b"
	return []session.Message{
		turn(1, session.RoleUser, "", "", "Our token-bucket limiter in internal/ratelimit/bucket.go loses refills under load: at 5k req/s about 3% of refills never happen. We're on Go 1.22 and the binary has to stay CGO-free. Any idea what's wrong?"),
		turn(2, session.RoleAssistant, "anthropic", opus, "The likeliest cause is a data race on the tokens field: refill() runs on a ticker goroutine while Take() decrements tokens without synchronisation. Run go test -race ./internal/ratelimit/... to confirm."),
		turn(3, session.RoleUser, "", "", "-race is clean. But pprof shows 41% of CPU in sync.(*Mutex).Lock inside refill(), and the ticker misses ticks while it waits."),
		turn(4, session.RoleAssistant, "anthropic", opus, "Then it isn't a race, it's lock contention. Make tokens an atomic.Int64 and refill with a compare-and-swap loop, keeping the mutex only for reloading the config."),
		turn(5, session.RoleUser, "", "", "Agreed, let's do the CAS. And don't suggest Redis, this has to stay in-process."),
		turn(6, session.RoleAssistant, "ollama", qwen, "Understood, in-process only. I'll rewrite refill() in internal/ratelimit/bucket.go with atomic.Int64 and CompareAndSwap, and add BenchmarkRefillUnderLoad to check the drop rate stays at 0%."),
	}
}

// planted are the facts a model continuing the conversation would need, each
// with words that show a note carried it. This is a smoke test of the
// prompt, not the evaluation: one conversation, scored by keyword.
var planted = []struct {
	name  string
	words []string
}{
	{"goal: refills lost in bucket.go", []string{"bucket.go"}},
	{"fact: 3% lost at 5k req/s", []string{"3%"}},
	{"constraint: CGO-free", []string{"cgo"}},
	{"fact: Go 1.22", []string{"1.22"}},
	{"fact: 41% of CPU in Mutex.Lock", []string{"41%"}},
	{"fact: not a race, lock contention", []string{"contention", "race is clean", "not a race"}},
	{"decision: atomic CAS in refill()", []string{"atomic", "compare-and-swap", "compareandswap"}},
	{"constraint: no Redis, in-process", []string{"redis"}},
	{"fact: BenchmarkRefillUnderLoad", []string{"benchmarkrefillunderload"}},
}

func TestLiveExtraction(t *testing.T) {
	var newSender func() liveSender
	var models []string
	switch os.Getenv("LIVE_PROVIDER") {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			t.Skip("set ANTHROPIC_API_KEY in your shell to run against Anthropic")
		}
		newSender = func() liveSender { return &anthropic{key: key} }
		models = []string{cheapModels["anthropic"]}
	default:
		newSender = func() liveSender { return &ollama{} }
		models = []string{"qwen2.5:3b", "llama3.2:3b", "qwen2.5-coder:3b"}
	}
	if env := os.Getenv("LIVE_MODELS"); env != "" {
		models = strings.Split(env, ",")
	}

	convo := liveConversation()
	windows := [][]session.Message{convo[:2], convo[2:4], convo[4:]}
	if os.Getenv("LIVE_MODE") == "switch" {
		windows = [][]session.Message{convo}
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			sender := newSender()
			x := NewModelExtractor(sender, model, costestimate.Heuristic{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			var existing []session.Note
			var in, out, actualIn, actualOut int
			var elapsed time.Duration
			for i, window := range windows {
				start := time.Now()
				pass, err := x.ExtractPass(ctx, window, existing)
				elapsed += time.Since(start)
				reply, ai, ao := sender.last()
				in, out, actualIn, actualOut = in+pass.Usage.InputTokens, out+pass.Usage.OutputTokens, actualIn+ai, actualOut+ao
				if err != nil {
					t.Logf("pass %d failed: %v\nraw reply:\n%s", i+1, err, reply)
					continue
				}
				t.Logf("pass %d:", i+1)
				for _, n := range pass.Notes {
					link := ""
					if n.LinksTo != nil {
						link = " -> " + quoted(*n.LinksTo, existing)
					}
					t.Logf("  + [%s] %s  (%s %s)%s", strings.Join(n.Tags, ", "), n.Content, n.Provider, n.Model, link)
				}
				for old, by := range pass.Supersedes {
					t.Logf("  ~ %s replaced by %s", quoted(old, existing), quoted(by, pass.Notes))
				}
				// Apply the pass as storage would, so the next pass sees it.
				for j := range existing {
					if by, ok := pass.Supersedes[existing[j].ID]; ok {
						existing[j].SupersededBy = &by
					}
				}
				existing = append(existing, pass.Notes...)
			}

			var text strings.Builder
			active := 0
			for _, n := range existing {
				if n.Active() {
					active++
					text.WriteString(strings.ToLower(n.Content) + "\n")
				}
			}
			var missed []string
			for _, p := range planted {
				hit := false
				for _, w := range p.words {
					hit = hit || strings.Contains(text.String(), w)
				}
				if !hit {
					missed = append(missed, p.name)
				}
			}
			counted := ""
			if actualIn > 0 {
				counted = fmt.Sprintf(" (provider counted %d in / %d out)", actualIn, actualOut)
			}
			t.Logf("SCORE %s, %d passes: recall %d/%d with %d active notes; heuristic %d in / %d out tokens%s; %s",
				model, len(windows), len(planted)-len(missed), len(planted), active, in, out, counted, elapsed.Round(time.Millisecond))
			for _, m := range missed {
				t.Logf("  missed %s", m)
			}
		})
	}
}

func quoted(id string, notes []session.Note) string {
	for _, n := range notes {
		if n.ID == id {
			return fmt.Sprintf("%q", n.Content)
		}
	}
	return id
}
