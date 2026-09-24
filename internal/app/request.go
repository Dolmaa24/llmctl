package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

// replyMsg carries the outcome of one request back to the update loop.
type replyMsg struct {
	seq      int
	provider string
	model    string
	reply    session.Message
	err      error
}

// request calls the adapter off the update loop, so a slow provider never
// blocks input (SRS NFR-1).
//
// A panic inside an adapter is converted into an error. The contract forbids
// adapters from panicking, but SRS NFR-5 requires that one failing adapter
// cannot take the application down, and this is the one place every adapter
// call passes through.
func request(ctx context.Context, seq int, a provider.Adapter, providerID, model string, history []session.Message) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = replyMsg{seq: seq, provider: providerID, model: model,
					err: fmt.Errorf("adapter panicked: %v", r)}
			}
		}()
		reply, err := a.SendMessage(ctx, model, history)
		return replyMsg{seq: seq, provider: providerID, model: model, reply: reply, err: err}
	}
}

// send appends the user's message and starts a request to the active provider.
func (m Model) send(text string) (tea.Model, tea.Cmd) {
	if m.inFlight {
		return m, nil
	}
	active, ok := m.activeProvider()
	if !ok {
		m.transcript.SetNotice("no active provider: select one in the providers pane")
		return m, nil
	}
	adapter, err := m.registry.Get(active.ID)
	if err != nil {
		m.transcript.SetNotice(err.Error())
		return m, nil
	}

	m.messages = append(m.messages, session.Message{
		SessionID:   m.sessionID,
		SequenceNum: len(m.messages),
		Role:        session.RoleUser,
		Content:     text,
		CreatedAt:   time.Now(),
	})
	m.transcript.SetMessages(m.messages)
	m.transcript.ScrollToBottom()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.inFlight = true
	m.reqSeq++
	m.composer.SetBusy(true)

	// INTEGRATION POINT (P4, session.HandoffBuilder): after a provider switch
	// this must send the HandoffPlan — the non-superseded notes plus the last
	// N turns — rather than the whole history. Sending everything is exactly
	// the naive replay this project exists to avoid; it stays only until the
	// handoff builder exists. The history is copied because the request runs
	// on another goroutine.
	history := append([]session.Message(nil), m.messages...)

	// Built into locals before returning: Go does not specify whether m is
	// copied before or after a call in the same return statement runs, so
	// `return m, m.transcript.SetPending(...)` could lose the pending state.
	spin := m.transcript.SetPending(active.ID, active.Model)
	req := request(ctx, m.reqSeq, adapter, active.ID, active.Model, history)
	return m, tea.Batch(spin, req)
}

// receive handles a completed request.
func (m Model) receive(r replyMsg) (tea.Model, tea.Cmd) {
	// A reply for a request the user already cancelled is dropped. Without
	// this, an adapter that ignores cancellation could slip a reply into the
	// conversation after the user had moved on.
	if !m.inFlight || r.seq != m.reqSeq {
		return m, nil
	}
	m.finishRequest()

	if r.err != nil {
		if errors.Is(r.err, context.Canceled) {
			m.transcript.SetNotice("request cancelled")
		} else {
			m.transcript.SetNotice(fmt.Sprintf("%s: %v", r.provider, r.err))
		}
		return m, nil
	}

	reply := r.reply
	reply.SessionID = m.sessionID
	reply.SequenceNum = len(m.messages)
	if reply.Role == "" {
		reply.Role = session.RoleAssistant
	}
	if reply.CreatedAt.IsZero() {
		reply.CreatedAt = time.Now()
	}
	// Trust the adapter's attribution when it gives one — a router such as
	// OpenRouter can answer with a different model than the one requested, and
	// the adapter is what knows. Fill it in only when missing, because an
	// unattributed turn would break SRS FR-3.3.
	if reply.Provider == "" {
		reply.Provider = r.provider
	}
	if reply.Model == "" {
		reply.Model = r.model
	}

	m.messages = append(m.messages, reply)
	m.transcript.SetMessages(m.messages)
	return m, nil
}

// cancelRequest abandons the in-flight request immediately.
//
// It frees the composer at once rather than waiting for the adapter to notice
// the cancellation. A provider that hangs, or an adapter that ignores its
// context, must never leave the user unable to send anything.
func (m *Model) cancelRequest() {
	if !m.inFlight {
		return
	}
	m.finishRequest()
	m.transcript.SetNotice("request cancelled")
}

func (m *Model) finishRequest() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.inFlight = false
	m.composer.SetBusy(false)
	m.transcript.ClearPending()
}
