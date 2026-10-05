package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
)

const defaultBaseURL = "http://127.0.0.1:11434"

// Compile-time check: Adapter must satisfy provider.Adapter.
var _ provider.Adapter = (*Adapter)(nil)

type Options struct {
	BaseURL   string // empty = OLLAMA_HOST, then the default
	Model     string // default model; Validate checks it is pulled
	NumCtx    int    // context window sent on every request (default 8192)
	KeepAlive string // how long models stay loaded (default "30m")
}

// Stats holds Ollama's own numbers from the last reply, for the report.
type Stats struct {
	PromptTokens   int
	OutputTokens   int
	PromptEvalTime time.Duration
	GenerateTime   time.Duration
	TotalTime      time.Duration
}

type Adapter struct {
	baseURL, model, keepAlive string
	numCtx                    int
	http                      *http.Client
	mu                        sync.Mutex
	last                      Stats
}

func New(o Options) *Adapter {
	base := o.BaseURL
	if base == "" {
		base = os.Getenv("OLLAMA_HOST")
	}
	if base == "" {
		base = defaultBaseURL
	}
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	base = withDefaultPort(base)
	if o.NumCtx <= 0 {
		o.NumCtx = 8192
	}
	if o.KeepAlive == "" {
		o.KeepAlive = "30m"
	}
	return &Adapter{
		baseURL: strings.TrimRight(base, "/"), model: o.Model,
		numCtx: o.NumCtx, keepAlive: o.KeepAlive,
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

// withDefaultPort adds Ollama's port 11434 to an http address that has none,
// because a bare host such as "localhost" would otherwise go to port 80.
func withDefaultPort(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.Port() != "" {
		return base
	}
	u.Host = net.JoinHostPort(u.Hostname(), "11434")
	return u.String()
}

func (a *Adapter) Name() string { return "ollama" }

// LastStats returns Ollama's token counts and timings from the last reply.
func (a *Adapter) LastStats() Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}

func (a *Adapter) call(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		// Running out of time is not the same as being unreachable: a model
		// still loading into memory needs patience, not a restart.
		var netErr net.Error
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return fmt.Errorf("request to Ollama was cancelled: %w", context.Canceled)
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
			return fmt.Errorf("Ollama at %s did not answer in time: %w", a.baseURL, context.DeadlineExceeded)
		}
		return fmt.Errorf("cannot reach Ollama at %s (is it running?): %w", a.baseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ollama http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, out)
}

func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := a.call(ctx, "GET", "/api/tags", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// Validate: the server is up, and the default model (if set) is pulled.
func (a *Adapter) Validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	models, err := a.ListModels(ctx)
	if err != nil {
		return err
	}
	if a.model == "" {
		return nil
	}
	want := a.model
	if !strings.Contains(want, ":") {
		want += ":latest"
	}
	for _, m := range models {
		if m == want {
			return nil
		}
	}
	return fmt.Errorf("ollama: model %q is not pulled (run: ollama pull %s)", a.model, a.model)
}

func (a *Adapter) GetEnvVars(ctx context.Context) (map[string]string, error) {
	return map[string]string{"OLLAMA_HOST": a.baseURL}, nil
}

// EstimateTokens uses a rough rule of about 4 characters per token.
func (a *Adapter) EstimateTokens(text string) int {
	n := len([]rune(text)) / 4
	if n == 0 && text != "" {
		return 1
	}
	return n
}

func (a *Adapter) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	if len(history) == 0 {
		return session.Message{}, errors.New("ollama: cannot send an empty history")
	}
	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msgs := make([]chatMsg, 0, len(history))
	total := 0
	for _, m := range history {
		total += a.EstimateTokens(m.Content)
		msgs = append(msgs, chatMsg{Role: string(m.Role), Content: m.Content})
	}
	// Ollama silently drops the oldest tokens, so refuse up front instead.
	if total+512 > a.numCtx {
		return session.Message{}, fmt.Errorf(
			"%w: ollama %s: about %d tokens will not fit in a window of %d",
			provider.ErrContextTooLarge, model, total, a.numCtx)
	}
	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"stream":     false,
		"think":      false,
		"keep_alive": a.keepAlive,
		"options":    map[string]any{"num_ctx": a.numCtx},
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		TotalDuration      int64 `json:"total_duration"`
		PromptEvalCount    int   `json:"prompt_eval_count"`
		PromptEvalDuration int64 `json:"prompt_eval_duration"`
		EvalCount          int   `json:"eval_count"`
		EvalDuration       int64 `json:"eval_duration"`
	}
	if err := a.call(ctx, "POST", "/api/chat", body, &out); err != nil {
		return session.Message{}, fmt.Errorf("ollama: send message: %w", err)
	}
	a.mu.Lock()
	a.last = Stats{
		PromptTokens:   out.PromptEvalCount,
		OutputTokens:   out.EvalCount,
		PromptEvalTime: time.Duration(out.PromptEvalDuration),
		GenerateTime:   time.Duration(out.EvalDuration),
		TotalTime:      time.Duration(out.TotalDuration),
	}
	a.mu.Unlock()
	return session.Message{
		Role:       session.RoleAssistant,
		Content:    out.Message.Content,
		Provider:   "ollama",
		Model:      model,
		TokenCount: out.EvalCount,
		CreatedAt:  time.Now(),
	}, nil
}
