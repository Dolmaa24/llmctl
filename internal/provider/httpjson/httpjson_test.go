package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fast() *Client {
	c := New()
	c.Wait = time.Millisecond
	return c
}

func TestRetriesRateLimitsAndServerErrorsThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	status, data, err := fast().Do(context.Background(), http.MethodGet, srv.URL, nil, nil)
	if err != nil || status != http.StatusOK || string(data) != `{"ok":true}` {
		t.Fatalf("got %d %q %v, want 200 after two retries", status, data, err)
	}
	if calls != 3 {
		t.Errorf("server saw %d calls, want 3", calls)
	}
}

func TestGivesUpAfterTheRetriesAndNeverRetriesAClientError(t *testing.T) {
	for status, want := range map[int]int{http.StatusInternalServerError: 3, http.StatusBadRequest: 1} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
		}))
		got, _, err := fast().Do(context.Background(), http.MethodGet, srv.URL, nil, nil)
		srv.Close()
		if err != nil || got != status {
			t.Errorf("status %d: got %d, %v", status, got, err)
		}
		if calls != want {
			t.Errorf("status %d: %d calls, want %d", status, calls, want)
		}
	}
}

func TestALongRetryAfterIsNotWaitedOut(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	status, _, err := fast().Do(context.Background(), http.MethodGet, srv.URL, nil, nil)
	if err != nil || status != http.StatusTooManyRequests || calls != 1 {
		t.Errorf("got %d, %v after %d calls; want the 429 handed back at once", status, err, calls)
	}
}

func TestSendsBodyAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Test") != "yes" {
			t.Errorf("headers not sent: %v", r.Header)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, _, err := fast().Do(context.Background(), http.MethodPost, srv.URL, map[string]string{"X-Test": "yes"}, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
}

func TestTransportErrorsSayWhatHappenedAndHideHeaders(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()
	secret := map[string]string{"Authorization": "Bearer sk-secret"}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := fast().Do(ctx, http.MethodGet, slow.URL, secret, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "did not answer in time") {
		t.Errorf("deadline: got %v", err)
	}

	gone, stop := context.WithCancel(context.Background())
	stop()
	_, _, err = fast().Do(gone, http.MethodGet, slow.URL, secret, nil)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancel: got %v", err)
	}

	_, _, err = fast().Do(context.Background(), http.MethodGet, "http://127.0.0.1:1", secret, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reach 127.0.0.1:1") {
		t.Errorf("refused: got %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("error leaks a header: %v", err)
	}
}

func TestContextTooLarge(t *testing.T) {
	yes := []string{"prompt is too long: 250000 tokens > 200000 maximum",
		"This endpoint's maximum context length is 8192 tokens", "exceeds the context window"}
	for _, m := range yes {
		if !ContextTooLarge(http.StatusBadRequest, m) {
			t.Errorf("%q not recognised", m)
		}
	}
	if !ContextTooLarge(http.StatusRequestEntityTooLarge, "") {
		t.Error("413 not recognised")
	}
	if ContextTooLarge(http.StatusBadRequest, "max_tokens: must be positive") {
		t.Error("an unrelated 400 was taken for a context error")
	}
}

func TestEstimateTokensAndSnip(t *testing.T) {
	if EstimateTokens("") != 0 || EstimateTokens("hi") != 1 || EstimateTokens(strings.Repeat("a", 400)) != 100 {
		t.Error("EstimateTokens is off")
	}
	if s := Snip([]byte(strings.Repeat("x \n", 400))); len(s) > 310 || strings.Contains(s, "\n") {
		t.Errorf("Snip left %d bytes: %q", len(s), s)
	}
}
