package mock

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// SessionsRepo is an in-memory storage.SessionsRepo.
type SessionsRepo struct {
	mu   sync.Mutex
	rows map[string]session.Session
}

func NewSessionsRepo() *SessionsRepo {
	return &SessionsRepo{rows: map[string]session.Session{}}
}

func (r *SessionsRepo) Create(ctx context.Context, s *session.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[s.ID] = *s
	return nil
}

func (r *SessionsRepo) Get(ctx context.Context, id string) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rows[id]
	if !ok {
		return nil, fmt.Errorf("mock: no session %q", id)
	}
	return &s, nil
}

func (r *SessionsRepo) UpdateActiveProvider(ctx context.Context, id, provider, model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rows[id]
	if !ok {
		return fmt.Errorf("mock: no session %q", id)
	}
	s.ActiveProvider, s.ActiveModel = provider, model
	r.rows[id] = s
	return nil
}

func (r *SessionsRepo) List(ctx context.Context) ([]session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]session.Session, 0, len(r.rows))
	for _, s := range r.rows {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// MessagesRepo is an in-memory storage.MessagesRepo.
type MessagesRepo struct {
	mu   sync.Mutex
	rows []session.Message
}

func NewMessagesRepo() *MessagesRepo { return &MessagesRepo{} }

func (r *MessagesRepo) Append(ctx context.Context, m *session.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, *m)
	return nil
}

func (r *MessagesRepo) ListBySession(ctx context.Context, sessionID string) ([]session.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []session.Message
	for _, m := range r.rows {
		if m.SessionID == sessionID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SequenceNum < out[j].SequenceNum })
	return out, nil
}

// NotesRepo is an in-memory storage.NotesRepo.
type NotesRepo struct {
	mu   sync.Mutex
	rows []session.Note
}

func NewNotesRepo() *NotesRepo { return &NotesRepo{} }

func (r *NotesRepo) Create(ctx context.Context, n *session.Note) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, *n)
	return nil
}

func (r *NotesRepo) ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]session.Note, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []session.Note
	for _, n := range r.rows {
		if n.SessionID != sessionID {
			continue
		}
		if !includeSuperseded && !n.Active() {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// MarkSuperseded requires the replacement to exist already, as the real
// table's superseded_by foreign key does.
func (r *NotesRepo) MarkSuperseded(ctx context.Context, noteID, supersededByID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.ContainsFunc(r.rows, func(n session.Note) bool { return n.ID == supersededByID }) {
		return fmt.Errorf("mock: no note %q to supersede with", supersededByID)
	}
	for i := range r.rows {
		if r.rows[i].ID == noteID {
			r.rows[i].SupersededBy = &supersededByID
			return nil
		}
	}
	return fmt.Errorf("mock: no note %q", noteID)
}

// SwitchEventsRepo is an in-memory storage.SwitchEventsRepo.
type SwitchEventsRepo struct {
	mu   sync.Mutex
	rows []session.SwitchEvent
}

func NewSwitchEventsRepo() *SwitchEventsRepo { return &SwitchEventsRepo{} }

func (r *SwitchEventsRepo) Create(ctx context.Context, e *session.SwitchEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, *e)
	return nil
}

func (r *SwitchEventsRepo) ListBySession(ctx context.Context, sessionID string) ([]session.SwitchEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []session.SwitchEvent
	for _, e := range r.rows {
		if e.SessionID == sessionID {
			out = append(out, e)
		}
	}
	return out, nil
}

// ExtractionRunsRepo is an in-memory notes.Ledger / storage.ExtractionRunsRepo.
type ExtractionRunsRepo struct {
	mu   sync.Mutex
	rows []session.ExtractionRun
	seq  int
}

func NewExtractionRunsRepo() *ExtractionRunsRepo { return &ExtractionRunsRepo{} }

func (r *ExtractionRunsRepo) Create(ctx context.Context, run *session.ExtractionRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.ID == "" {
		r.seq++
		run.ID = fmt.Sprintf("run-%d", r.seq)
	}
	r.rows = append(r.rows, *run)
	return nil
}

func (r *ExtractionRunsRepo) ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []session.ExtractionRun
	for _, run := range r.rows {
		if run.SessionID == sessionID {
			out = append(out, run)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
