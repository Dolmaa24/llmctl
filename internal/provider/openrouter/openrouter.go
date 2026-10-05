// Package openrouter is the provider adapter for OpenRouter, which speaks the
// OpenAI chat-completions format in front of many models.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/provider/httpjson"
	"github.com/Dolmaa24/llmctl/internal/session"
)

const (
	name           = "openrouter"
	defaultBaseURL = "https://openrouter.ai/api/v1"
)

// Compile-time check: Adapter must satisfy provider.Adapter.
var _ provider.Adapter = (*Adapter)(nil)

// KeySource is the one method of config.SecretStore this adapter needs. The
// key is fetched for each request and never kept on the adapter.
type KeySource interface {
	GetAPIKey(providerID string) (string, error)
}

type Options struct {
	Keys    KeySource
	BaseURL string // empty = the public API
	Model   string // default model; Validate checks it is listed
}

type Adapter struct {
	keys    KeySource
	baseURL string
	model   string
	http    *httpjson.Client
}

func New(o Options) *Adapter {
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	return &Adapter{keys: o.Keys, baseURL: strings.TrimRight(o.BaseURL, "/"), model: o.Model, http: httpjson.New()}
}

func (a *Adapter) Name() string { return name }

func (a *Adapter) key() (string, error) {
	if a.keys == nil {
		return "", errors.New("openrouter: no key store configured")
	}
	k, err := a.keys.GetAPIKey(name)
	if err != nil {
		return "", fmt.Errorf("openrouter: reading the API key: %w", err)
	}
	if k == "" {
		return "", errors.New("openrouter: no API key saved")
	}
	return k, nil
}

// apiErr is the error object OpenRouter returns. It can arrive with a failing
// status or inside a 200, when the model behind OpenRouter failed.
type apiErr struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}

func (a *Adapter) call(ctx context.Context, method, path string, withKey bool, body, out any) error {
	headers := map[string]string{"X-Title": "llmctl"}
	if withKey {
		k, err := a.key()
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + k
	}
	status, data, err := a.http.Do(ctx, method, a.baseURL+path, headers, body)
	if err != nil {
		return fmt.Errorf("openrouter: %w", err)
	}
	var envelope struct {
		Error *apiErr `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	if status >= 300 || envelope.Error != nil {
		msg := httpjson.Snip(data)
		if envelope.Error != nil && envelope.Error.Message != "" {
			msg = envelope.Error.Message
		}
		return apiError(status, msg)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("openrouter: unreadable reply: %w", err)
	}
	return nil
}

// apiError turns an error response into a message for the diagnostics panel.
func apiError(status int, msg string) error {
	switch {
	case httpjson.ContextTooLarge(status, msg):
		return fmt.Errorf("%w: openrouter: %s", provider.ErrContextTooLarge, msg)
	case status == http.StatusUnauthorized:
		return errors.New("openrouter: the API key was rejected (http 401)")
	case status == http.StatusPaymentRequired:
		return fmt.Errorf("openrouter: not enough credit for this request (http 402): %s", msg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("openrouter: rate limited, try again shortly (http 429): %s", msg)
	case status >= 500:
		return fmt.Errorf("openrouter: the service is having trouble (http %d): %s", status, msg)
	}
	return fmt.Errorf("openrouter: %s (http %d)", msg, status)
}

// ListModels returns every model OpenRouter offers. The list is public, so it
// works before a key has been saved.
func (a *Adapter) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := a.call(ctx, http.MethodGet, "/models", false, nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Validate: the key is accepted, and the default model (if set) is listed.
// Neither request generates text, so validating costs nothing.
func (a *Adapter) Validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var ignored json.RawMessage
	if err := a.call(ctx, http.MethodGet, "/key", true, nil, &ignored); err != nil {
		return err
	}
	if a.model == "" {
		return nil
	}
	models, err := a.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m == a.model {
			return nil
		}
	}
	return fmt.Errorf("openrouter: model %q is not offered", a.model)
}

func (a *Adapter) GetEnvVars(ctx context.Context) (map[string]string, error) {
	k, err := a.key()
	if err != nil {
		return nil, err
	}
	return map[string]string{"OPENROUTER_API_KEY": k}, nil
}

func (a *Adapter) EstimateTokens(text string) int { return httpjson.EstimateTokens(text) }

func (a *Adapter) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msgs := make([]chatMsg, 0, len(history))
	for _, m := range history {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		msgs = append(msgs, chatMsg{Role: string(m.Role), Content: m.Content})
	}
	if len(msgs) == 0 {
		return session.Message{}, errors.New("openrouter: cannot send an empty history")
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string  `json:"finish_reason"`
			Error        *apiErr `json:"error"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	body := map[string]any{"model": model, "messages": msgs}
	if err := a.call(ctx, http.MethodPost, "/chat/completions", true, body, &out); err != nil {
		return session.Message{}, err
	}
	if len(out.Choices) == 0 {
		return session.Message{}, fmt.Errorf("openrouter: %s returned no reply", model)
	}
	choice := out.Choices[0]
	if choice.Error != nil {
		return session.Message{}, apiError(http.StatusOK, choice.Error.Message)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return session.Message{}, fmt.Errorf("openrouter: %s returned no text (finish reason %q)", model, choice.FinishReason)
	}
	return session.Message{
		Role:       session.RoleAssistant,
		Content:    choice.Message.Content,
		Provider:   name,
		Model:      model,
		TokenCount: out.Usage.CompletionTokens,
		CreatedAt:  time.Now(),
	}, nil
}
