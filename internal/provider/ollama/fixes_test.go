package ollama

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
)

func TestWithDefaultPort(t *testing.T) {
	cases := map[string]string{
		"http://localhost":      "http://localhost:11434",
		"http://localhost:8080": "http://localhost:8080",
		"http://[::1]":          "http://[::1]:11434",
		"https://example.com":   "https://example.com",
	}
	for in, want := range cases {
		if got := withDefaultPort(in); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSendMessageEmptyHistory(t *testing.T) {
	a := New(Options{})
	if _, err := a.SendMessage(context.Background(), "m", nil); err == nil {
		t.Fatal("expected an error for an empty history")
	}
}

func TestContextTooLargeSentinelFirst(t *testing.T) {
	a := New(Options{NumCtx: 1024})
	big := session.Message{Role: session.RoleUser, Content: strings.Repeat("word ", 5000)}
	_, err := a.SendMessage(context.Background(), "m", []session.Message{big})
	if !errors.Is(err, provider.ErrContextTooLarge) {
		t.Fatalf("expected ErrContextTooLarge, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), provider.ErrContextTooLarge.Error()) {
		t.Errorf("sentinel should come first, got %q", err.Error())
	}
}

// A model still loading can outlast the deadline while Ollama is running
// fine, so the error must not tell the user it cannot be reached.
func TestDeadlineIsNotReportedAsUnreachable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(Options{BaseURL: srv.URL}).SendMessage(ctx, "m",
		[]session.Message{{Role: session.RoleUser, Content: "hi"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a deadline error, got %v", err)
	}
	if strings.Contains(err.Error(), "cannot reach") || !strings.Contains(err.Error(), "did not answer in time") {
		t.Errorf("deadline worded as unreachable: %q", err.Error())
	}
}
