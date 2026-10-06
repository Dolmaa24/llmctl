package ollama

import (
	"context"
	"errors"
	"strings"
	"testing"

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
