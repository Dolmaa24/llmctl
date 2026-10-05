package anthropic

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

const testKey = "sk-ant-test-0000"

type keys map[string]string

func (k keys) GetAPIKey(id string) (string, error) { return k[id], nil }

// serve starts a fake API and returns an adapter pointed at it. Every request
// must carry the key and the version header.
func serve(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("request without key or version header: %v", r.Header)
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	a := New(Options{Keys: keys{name: testKey}, BaseURL: srv.URL, Model: "claude-haiku-4-5"})
	a.http.Wait = time.Millisecond
	return a
}

func user(s string) session.Message { return session.Message{Role: session.RoleUser, Content: s} }
func asst(s string) session.Message { return session.Message{Role: session.RoleAssistant, Content: s} }
func sys(s string) session.Message  { return session.Message{Role: session.RoleSystem, Content: s} }

func TestSendMessage(t *testing.T) {
	var got map[string]any
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"Hello"},{"type":"text","text":" there"}],
			"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":7}}`))
	})
	reply, err := a.SendMessage(context.Background(), "claude-haiku-4-5", []session.Message{sys("Be brief."), user("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Hello there" || reply.Role != session.RoleAssistant || reply.Provider != "anthropic" ||
		reply.Model != "claude-haiku-4-5" || reply.TokenCount != 7 || reply.CreatedAt.IsZero() {
		t.Errorf("unexpected reply %+v", reply)
	}
	if got["model"] != "claude-haiku-4-5" || got["system"] != "Be brief." || got["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("unexpected request body %v", got)
	}
	// Sampling and thinking settings differ by model and several are rejected
	// outright on current ones, so the adapter sends none.
	for _, field := range []string{"temperature", "top_p", "top_k", "thinking"} {
		if _, sent := got[field]; sent {
			t.Errorf("request carries %q", field)
		}
	}
}

func TestHistoryIsReshapedForTheAPI(t *testing.T) {
	// A handoff: notes as a system message, then recent turns that happen to
	// begin with an assistant reply, with two user turns in a row at the end.
	system, turns, err := toRequest([]session.Message{
		sys("Notes: use atomic CAS."), asst("Here is the loop."), user("Thanks."), asst("Anything else?"),
		user("Yes."), user("Add a benchmark."), {Role: session.RoleUser, Content: "  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(system, "Notes: use atomic CAS.") || !strings.Contains(system, "Here is the loop.") {
		t.Errorf("system text lost the notes or the leading reply: %q", system)
	}
	want := []turn{{"user", "Thanks."}, {"assistant", "Anything else?"}, {"user", "Yes.\n\nAdd a benchmark."}}
	if len(turns) != len(want) {
		t.Fatalf("turns = %+v, want %+v", turns, want)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Errorf("turn %d = %+v, want %+v", i, turns[i], want[i])
		}
	}
}

func TestHistoryThatCannotBeSentIsRefusedBeforeAnyRequest(t *testing.T) {
	a := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("a request was sent") })
	bad := map[string][]session.Message{
		"empty":             nil,
		"system only":       {sys("notes")},
		"ends on assistant": {user("hi"), asst("hello")},
	}
	for name, history := range bad {
		if _, err := a.SendMessage(context.Background(), "m", history); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestErrorsAreWordedForTheUser(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		tooLarge bool
		contains string
	}{
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens > 200000 maximum"}}`, true, "250000"},
		{413, `{"type":"error","error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`, true, "maximum size"},
		{401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, false, "key was rejected"},
		{404, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-nope"}}`, false, "claude-nope"},
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: too large"}}`, false, "max_tokens: too large"},
		{529, `not json`, false, "having trouble"},
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
			t.Errorf("status %d: ErrContextTooLarge = %v, want %v", c.status, !c.tooLarge, c.tooLarge)
		}
		if c.tooLarge && !strings.HasPrefix(err.Error(), provider.ErrContextTooLarge.Error()) {
			t.Errorf("status %d: sentinel should come first, got %q", c.status, err)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("status %d: error leaks the key: %v", c.status, err)
		}
	}
}

func TestRefusalAndEmptyReplyAreErrors(t *testing.T) {
	for body, want := range map[string]string{
		`{"content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"}}`: "declined this request (cyber)",
		`{"content":[{"type":"thinking","thinking":""}],"stop_reason":"max_tokens"}`:                  "returned no text",
	} {
		a := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		_, err := a.SendMessage(context.Background(), "m", []session.Message{user("hi")})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want it to mention %q", err, want)
		}
	}
}

func TestListModelsFollowsPages(t *testing.T) {
	a := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5-5"},{"id":"claude-sonnet-5-5"}],"has_more":true,"last_id":"claude-sonnet-5-5"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-haiku-4-5"}],"has_more":false,"last_id":"claude-haiku-4-5"}`))
	})
	models, err := a.ListModels(context.Background())
	if err != nil || strings.Join(models, ",") != "claude-opus-5-5,claude-sonnet-5-5,claude-haiku-4-5" {
		t.Errorf("ListModels = %v, %v", models, err)
	}
}

func TestValidate(t *testing.T) {
	ok := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/claude-haiku-4-5" {
			t.Errorf("Validate asked for %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"claude-haiku-4-5"}`))
	})
	if err := ok.Validate(context.Background()); err != nil {
		t.Errorf("valid key and model: %v", err)
	}

	rejected := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	})
	if err := rejected.Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("rejected key: got %v", err)
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
	if err != nil || len(env) != 1 || env["ANTHROPIC_API_KEY"] != testKey {
		t.Errorf("default base URL: %v, %v", env, err)
	}
	env, _ = New(Options{Keys: keys{name: testKey}, BaseURL: "https://proxy.example.com/"}).GetEnvVars(context.Background())
	if env["ANTHROPIC_BASE_URL"] != "https://proxy.example.com" {
		t.Errorf("custom base URL not exported: %v", env)
	}
}
