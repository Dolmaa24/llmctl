// Package anthropic is the provider adapter for the Anthropic Messages API.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/provider/httpjson"
	"github.com/Dolmaa24/llmctl/internal/session"
)

const (
	name             = "anthropic"
	defaultBaseURL   = "https://api.anthropic.com"
	apiVersion       = "2023-06-01"
	defaultMaxTokens = 16000
)

// Compile-time check: Adapter must satisfy provider.Adapter.
var _ provider.Adapter = (*Adapter)(nil)

// KeySource is the one method of config.SecretStore this adapter needs. The
// key is fetched for each request and never kept on the adapter.
type KeySource interface {
	GetAPIKey(providerID string) (string, error)
}

type Options struct {
	Keys      KeySource
	BaseURL   string // empty = the public API
	Model     string // default model; Validate checks this key can use it
	MaxTokens int    // cap on one reply (default 16000)
}

type Adapter struct {
	keys      KeySource
	baseURL   string
	model     string
	maxTokens int
	http      *httpjson.Client
}

func New(o Options) *Adapter {
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = defaultMaxTokens
	}
	return &Adapter{
		keys: o.Keys, baseURL: strings.TrimRight(o.BaseURL, "/"),
		model: o.Model, maxTokens: o.MaxTokens, http: httpjson.New(),
	}
}

func (a *Adapter) Name() string { return name }

func (a *Adapter) key() (string, error) {
	if a.keys == nil {
		return "", errors.New("anthropic: no key store configured")
	}
	k, err := a.keys.GetAPIKey(name)
	if err != nil {
		return "", fmt.Errorf("anthropic: reading the API key: %w", err)
	}
	if k == "" {
		return "", errors.New("anthropic: no API key saved")
	}
	return k, nil
}

func (a *Adapter) call(ctx context.Context, method, path string, body, out any) error {
	k, err := a.key()
	if err != nil {
		return err
	}
	headers := map[string]string{"x-api-key": k, "anthropic-version": apiVersion}
	status, data, err := a.http.Do(ctx, method, a.baseURL+path, headers, body)
	if err != nil {
		return fmt.Errorf("anthropic: %w", err)
	}
	if status >= 300 {
		return apiError(status, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("anthropic: unreadable reply: %w", err)
	}
	return nil
}

// apiError turns an error response into a message for the diagnostics panel.
func apiError(status int, data []byte) error {
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &body)
	msg := body.Error.Message
	if msg == "" {
		msg = httpjson.Snip(data)
	}
	switch {
	case httpjson.ContextTooLarge(status, msg):
		return fmt.Errorf("%w: anthropic: %s", provider.ErrContextTooLarge, msg)
	case status == http.StatusUnauthorized:
		return errors.New("anthropic: the API key was rejected (http 401)")
	case status == http.StatusNotFound:
		return fmt.Errorf("anthropic: not found, or not available to this key (http 404): %s", msg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("anthropic: rate limited, try again shortly (http 429): %s", msg)
	case status >= 500:
		return fmt.Errorf("anthropic: the service is having trouble (http %d): %s", status, msg)
	}
	return fmt.Errorf("anthropic: %s (http %d)", msg, status)
}

// ListModels returns every model this key can use.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	var ids []string
	after := ""
	for {
		q := url.Values{"limit": {"1000"}}
		if after != "" {
			q.Set("after_id", after)
		}
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := a.call(ctx, http.MethodGet, "/v1/models?"+q.Encode(), nil, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			ids = append(ids, m.ID)
		}
		if !page.HasMore || page.LastID == "" {
			return ids, nil
		}
		after = page.LastID
	}
}

// Validate: the key is accepted, and the default model (if set) is one it can
// use. Neither request generates text, so validating costs nothing.
func (a *Adapter) Validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var ignored json.RawMessage
	if a.model == "" {
		return a.call(ctx, http.MethodGet, "/v1/models?limit=1", nil, &ignored)
	}
	return a.call(ctx, http.MethodGet, "/v1/models/"+url.PathEscape(a.model), nil, &ignored)
}

func (a *Adapter) GetEnvVars(ctx context.Context) (map[string]string, error) {
	k, err := a.key()
	if err != nil {
		return nil, err
	}
	env := map[string]string{"ANTHROPIC_API_KEY": k}
	if a.baseURL != defaultBaseURL {
		env["ANTHROPIC_BASE_URL"] = a.baseURL
	}
	return env, nil
}

func (a *Adapter) EstimateTokens(text string) int { return httpjson.EstimateTokens(text) }

type turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// toRequest reshapes a history into what the Messages API accepts: system text
// travels outside the turns, turns alternate, and the first turn is the user's.
//
// A handoff does not arrive in that shape. It is a system message of notes
// followed by the most recent turns, which may begin with an assistant reply.
// That leading reply is kept as context in the system text rather than dropped,
// because losing it would silently shorten what the user handed over.
func toRequest(history []session.Message) (system string, turns []turn, err error) {
	var sys []string
	for _, m := range history {
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		switch {
		case m.Role == session.RoleSystem:
			sys = append(sys, text)
		case m.Role == session.RoleAssistant && len(turns) == 0:
			sys = append(sys, "Earlier in this conversation the assistant said:\n"+text)
		case len(turns) > 0 && turns[len(turns)-1].Role == string(m.Role):
			turns[len(turns)-1].Content += "\n\n" + text
		default:
			turns = append(turns, turn{Role: string(m.Role), Content: text})
		}
	}
	if len(turns) == 0 {
		return "", nil, errors.New("anthropic: cannot send a history with no user message")
	}
	if turns[len(turns)-1].Role != string(session.RoleUser) {
		return "", nil, errors.New("anthropic: the last message must come from the user")
	}
	return strings.Join(sys, "\n\n"), turns, nil
}

func (a *Adapter) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	system, turns, err := toRequest(history)
	if err != nil {
		return session.Message{}, err
	}
	body := map[string]any{"model": model, "max_tokens": a.maxTokens, "messages": turns}
	if system != "" {
		body["system"] = system
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason  string `json:"stop_reason"`
		StopDetails *struct {
			Category string `json:"category"`
		} `json:"stop_details"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := a.call(ctx, http.MethodPost, "/v1/messages", body, &out); err != nil {
		return session.Message{}, err
	}
	if out.StopReason == "refusal" {
		why := ""
		if out.StopDetails != nil && out.StopDetails.Category != "" {
			why = " (" + out.StopDetails.Category + ")"
		}
		return session.Message{}, fmt.Errorf("anthropic: %s declined this request%s", model, why)
	}
	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return session.Message{}, fmt.Errorf("anthropic: %s returned no text (stop reason %q)", model, out.StopReason)
	}
	return session.Message{
		Role:       session.RoleAssistant,
		Content:    text.String(),
		Provider:   name,
		Model:      model,
		TokenCount: out.Usage.OutputTokens,
		CreatedAt:  time.Now(),
	}, nil
}
