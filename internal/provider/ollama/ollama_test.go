package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
)

func TestSendMessageSetsOptions(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"content":"hi"},"prompt_eval_count":5,"eval_count":2}`))
	}))
	defer srv.Close()

	a := New(Options{BaseURL: srv.URL, NumCtx: 4096, KeepAlive: "30m"})
	reply, err := a.SendMessage(context.Background(), "m",
		[]session.Message{{Role: session.RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "hi" {
		t.Fatalf("unexpected reply %q", reply.Content)
	}
	if got["keep_alive"] != "30m" {
		t.Fatalf("keep_alive not sent: %v", got["keep_alive"])
	}
	opts, _ := got["options"].(map[string]any)
	if opts["num_ctx"] != float64(4096) {
		t.Fatalf("num_ctx not sent: %v", opts["num_ctx"])
	}
	if a.LastStats().PromptTokens != 5 {
		t.Fatalf("stats not kept: %+v", a.LastStats())
	}
}

func TestTooLarge(t *testing.T) {
	a := New(Options{BaseURL: "http://127.0.0.1:1", NumCtx: 1024})
	big := session.Message{Role: session.RoleUser, Content: strings.Repeat("word ", 5000)}
	_, err := a.SendMessage(context.Background(), "m", []session.Message{big})
	if !errors.Is(err, provider.ErrContextTooLarge) {
		t.Fatalf("expected ErrContextTooLarge, got %v", err)
	}
}
