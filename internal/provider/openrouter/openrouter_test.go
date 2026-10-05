package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
)

const testKey = "sk-or-test-0000"

type keys map[string]string

func (k keys) GetAPIKey(id string) (string, error) { return k[id], nil }

func serve(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	a := New(Options{Keys: keys{name: testKey}, BaseURL: srv.URL, Model: "meta-llama/llama-3.1-70b"})
	a.http.Wait = time.Millisecond
	return a
}

func user(s string) session.Message { return session.Message{Role: session.RoleUser, Content: s} }

func TestSendMessage(t *testing.T) {
	var got struct {
		Model    string
		Messages []struct{ Role, Content string }
	}
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("key not sent as a bearer token: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":9,"completion_tokens":4}}`))
	})
	history := []session.Message{
		{Role: session.RoleSystem, Content: "Notes: use atomic CAS."},
		{Role: session.RoleAssistant, Content: "Here is the loop."},
		{Role: session.RoleUser, Content: " "},
		user("Add a benchmark."),
	}
	reply, err := a.SendMessage(context.Background(), "meta-llama/llama-3.1-70b", history)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Hello" || reply.Role != session.RoleAssistant || reply.Provider != "openrouter" ||
		reply.Model != "meta-llama/llama-3.1-70b" || reply.TokenCount != 4 || reply.CreatedAt.IsZero() {
		t.Errorf("unexpected reply %+v", reply)
	}
	if got.Model != "meta-llama/llama-3.1-70b" || len(got.Messages) != 3 ||
		got.Messages[0].Role != "system" || got.Messages[1].Role != "assistant" || got.Messages[2].Content != "Add a benchmark." {
		t.Errorf("unexpected request body %+v", got)
	}
}

func TestErrorsAreWordedForTheUser(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		tooLarge bool
		contains string
	}{
		{400, `{"error":{"code":400,"message":"This endpoint's maximum context length is 8192 tokens. However, you requested about 9214 tokens."}}`, true, "9214"},
		{200, `{"error":{"code":"context_length_exceeded","message":"maximum context length is 8192 tokens"}}`, true, "8192"},
		{401, `{"error":{"code":401,"message":"No auth credentials found"}}`, false, "key was rejected"},
		{402, `{"error":{"code":402,"message":"Insufficient credits"}}`, false, "not enough credit"},
		{404, `{"error":{"code":404,"message":"No endpoints found for nope/nope"}}`, false, "nope/nope"},
		{502, `<html>bad gateway</html>`, false, "having trouble"},
		{200, `{"choices":[]}`, false, "returned no reply"},
		{200, `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`, false, "returned no text"},
		{200, `{"choices":[{"error":{"code":502,"message":"upstream model failed"},"message":{"content":""}}]}`, false, "upstream model failed"},
	}
	for _, c := range cases {
		a := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		_, err := a.SendMessage(context.Background(), "m", []session.Message{user("hi")})
		if err == nil || !strings.Contains(err.Error(), c.contains) {
			t.Errorf("status %d: got %v, want it to mention %q", c.status, err, c.contains)
			continue
		}
		if errors.Is(err, provider.ErrContextTooLarge) != c.tooLarge {
			t.Errorf("status %d %s: ErrContextTooLarge = %v, want %v", c.status, c.contains, !c.tooLarge, c.tooLarge)
		}
		if c.tooLarge && !strings.HasPrefix(err.Error(), provider.ErrContextTooLarge.Error()) {
			t.Errorf("status %d: sentinel should come first, got %q", c.status, err)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("status %d: error leaks the key: %v", c.status, err)
		}
	}
}

func TestEmptyHistoryIsRefusedBeforeAnyRequest(t *testing.T) {
	a := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("a request was sent") })
	for _, history := range [][]session.Message{nil, {user("  ")}} {
		if _, err := a.SendMessage(context.Background(), "m", history); err == nil {
			t.Errorf("accepted %+v", history)
		}
	}
}

func TestListModelsNeedsNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request %s, auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic/claude-haiku-4.5"},{"id":"meta-llama/llama-3.1-70b"}]}`))
	}))
	defer srv.Close()
	models, err := New(Options{BaseURL: srv.URL}).ListModels(context.Background())
	if err != nil || strings.Join(models, ",") != "anthropic/claude-haiku-4.5,meta-llama/llama-3.1-70b" {
		t.Errorf("ListModels = %v, %v", models, err)
	}
}

func TestValidate(t *testing.T) {
	handler := func(keyStatus int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/key":
				w.WriteHeader(keyStatus)
				_, _ = w.Write([]byte(`{"data":{"label":"test"}}`))
			case "/models":
				_, _ = w.Write([]byte(`{"data":[{"id":"meta-llama/llama-3.1-70b"}]}`))
			default:
				t.Errorf("unexpected path %s", r.URL.Path)
			}
		}
	}
	if err := serve(t, handler(http.StatusOK)).Validate(context.Background()); err != nil {
		t.Errorf("valid key and model: %v", err)
	}
	if err := serve(t, handler(http.StatusUnauthorized)).Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("rejected key: got %v", err)
	}
	unlisted := serve(t, handler(http.StatusOK))
	unlisted.model = "nope/nope"
	if err := unlisted.Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "not offered") {
		t.Errorf("unlisted model: got %v", err)
	}
}

func TestNoKeyMeansNoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("a request was sent") }))
	defer srv.Close()
	for _, a := range []*Adapter{New(Options{BaseURL: srv.URL}), New(Options{Keys: keys{}, BaseURL: srv.URL})} {
		if _, err := a.SendMessage(context.Background(), "m", []session.Message{user("hi")}); err == nil {
			t.Error("sent without a key")
		}
		if err := a.Validate(context.Background()); err == nil {
			t.Error("validated without a key")
		}
		if _, err := a.GetEnvVars(context.Background()); err == nil {
			t.Error("returned env vars without a key")
		}
	}
}

func TestGetEnvVars(t *testing.T) {
	env, err := New(Options{Keys: keys{name: testKey}}).GetEnvVars(context.Background())
	if err != nil || len(env) != 1 || env["OPENROUTER_API_KEY"] != testKey {
		t.Errorf("GetEnvVars = %v, %v", env, err)
	}
}
