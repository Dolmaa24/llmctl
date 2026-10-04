package mock

import (
	"context"
	"os/exec"
	"time"

	"github.com/Dolmaa24/llmctl/internal/doctor"
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/shell"
)

// Detector is a configurable shell.Detector.
type Detector struct {
	Kind shell.Kind
	Err  error
}

func (d *Detector) Detect() (shell.Kind, error) {
	if d.Kind == "" {
		return shell.KindUnknown, d.Err
	}
	return d.Kind, d.Err
}

// Renderer is a configurable shell.Renderer.
type Renderer struct {
	Out string
	Err error
}

func (r *Renderer) Snippet(kind shell.Kind, vars map[string]string) (string, error) {
	return r.Out, r.Err
}

// Launcher is a configurable shell.Launcher that records what it was asked to
// run instead of starting a process.
type Launcher struct {
	Calls []LaunchCall
	Err   error
}

type LaunchCall struct {
	Vars map[string]string
	Name string
	Args []string
}

func (l *Launcher) Launch(ctx context.Context, vars map[string]string, name string, args ...string) (*exec.Cmd, error) {
	l.Calls = append(l.Calls, LaunchCall{Vars: vars, Name: name, Args: args})
	if l.Err != nil {
		return nil, l.Err
	}
	return exec.CommandContext(ctx, name, args...), nil
}

// Check is a configurable doctor.Check.
type Check struct {
	NameValue string
	Result    doctor.CheckResult
	Delay     time.Duration
}

func (c *Check) Name() string { return c.NameValue }

func (c *Check) Run(ctx context.Context) doctor.CheckResult {
	if c.Delay > 0 {
		select {
		case <-time.After(c.Delay):
		case <-ctx.Done():
			return doctor.CheckResult{
				Name:      c.NameValue,
				Status:    doctor.StatusWarn,
				Message:   "timed out",
				CheckedAt: time.Now(),
			}
		}
	}
	res := c.Result
	res.Name = c.NameValue
	if res.CheckedAt.IsZero() {
		res.CheckedAt = time.Now()
	}
	if res.Status == "" {
		res.Status = doctor.StatusOK
	}
	return res
}

// Extractor is a configurable notes.Extractor.
type Extractor struct {
	Notes []session.Note
	Err   error
	Calls int
}

func (e *Extractor) Extract(ctx context.Context, recent []session.Message, existing []session.Note) ([]session.Note, error) {
	e.Calls++
	return e.Notes, e.Err
}
