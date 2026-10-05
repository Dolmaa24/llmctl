// Package httpjson is the small HTTP helper shared by the hosted provider
// adapters. It sends one JSON request, retries the failures that are worth
// retrying, and words a transport failure so the user can tell a slow provider
// from an unreachable one.
//
// It never logs, and no error it returns contains a request header, so an API
// key passed in headers cannot leak through it (SRS NFR-4).
package httpjson

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
	"strconv"
	"strings"
	"time"
)

const (
	maxBodyBytes  = 8 << 20
	maxRetryAfter = 30 * time.Second
)

// Client sends JSON requests to one provider.
type Client struct {
	HTTP    *http.Client
	Retries int           // extra attempts after a 429 or a 5xx
	Wait    time.Duration // pause before the first retry; doubles each time
}

// New returns a Client with the defaults the hosted adapters use.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Minute}, Retries: 2, Wait: time.Second}
}

// Do sends body as JSON (nil for no body) and returns the status and the raw
// response. A non-2xx status is not an error here: each provider words its
// own, so the caller decides what it means.
func (c *Client) Do(ctx context.Context, method, rawURL string, headers map[string]string, body any) (int, []byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return 0, nil, err
		}
	}
	wait := c.Wait
	for attempt := 0; ; attempt++ {
		status, data, retryAfter, err := c.once(ctx, method, rawURL, headers, payload)
		if err != nil {
			return 0, nil, err
		}
		retryable := status == http.StatusTooManyRequests || status >= 500
		if !retryable || attempt >= c.Retries {
			return status, data, nil
		}
		pause := wait
		if retryAfter > 0 {
			pause = retryAfter
		}
		if pause > maxRetryAfter {
			return status, data, nil
		}
		select {
		case <-ctx.Done():
			return 0, nil, transportError(ctx, rawURL, ctx.Err())
		case <-time.After(pause):
		}
		wait *= 2
	}
}

func (c *Client) once(ctx context.Context, method, rawURL string, headers map[string]string, payload []byte) (int, []byte, time.Duration, error) {
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return 0, nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, 0, transportError(ctx, rawURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return 0, nil, 0, transportError(ctx, rawURL, err)
	}
	var retryAfter time.Duration
	if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
		retryAfter = time.Duration(s) * time.Second
	}
	return resp.StatusCode, data, retryAfter, nil
}

// transportError separates "gave up waiting" from "could not connect". The two
// need different fixes, and telling a user whose provider is merely slow that
// it cannot be reached sends them looking in the wrong place.
func transportError(ctx context.Context, rawURL string, err error) error {
	host := rawURL
	if u, perr := url.Parse(rawURL); perr == nil && u.Host != "" {
		host = u.Host
	}
	var netErr net.Error
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("request to %s was cancelled: %w", host, context.Canceled)
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Errorf("%s did not answer in time: %w", host, context.DeadlineExceeded)
	}
	return fmt.Errorf("cannot reach %s: %w", host, err)
}

// Snip shortens a response body for use in an error message.
func Snip(data []byte) string {
	s := strings.Join(strings.Fields(string(data)), " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// ContextTooLarge reports whether a provider's error says the conversation
// does not fit the model. Providers have no shared code for this, only prose.
func ContextTooLarge(status int, message string) bool {
	if status == http.StatusRequestEntityTooLarge {
		return true
	}
	m := strings.ToLower(message)
	for _, phrase := range []string{"prompt is too long", "context length", "context window", "maximum context", "too many tokens"} {
		if strings.Contains(m, phrase) {
			return true
		}
	}
	return false
}

// EstimateTokens is the rough rule the adapters share: about four characters
// per token, and never zero for text that is not empty.
func EstimateTokens(text string) int {
	n := len([]rune(text)) / 4
	if n == 0 && text != "" {
		return 1
	}
	return n
}
