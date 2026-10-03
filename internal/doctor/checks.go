// Package doctor runs environment health checks. Along with internal/shell it
// is one of only two packages permitted to contain OS-specific branching.
package doctor

import (
	"context"
	"time"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// CheckTimeout is the hard ceiling for any individual check. A check that
// exceeds it reports StatusWarn rather than blocking the UI (SRS NFR-2).
const CheckTimeout = 2 * time.Second

// CheckResult is what a check reports to the status bar or the doctor command.
type CheckResult struct {
	Name      string
	Status    Status
	Message   string // human-readable detail
	CheckedAt time.Time
}

// Check is one diagnostic probe, e.g. "wsl", "network", "auth:anthropic".
type Check interface {
	Name() string

	// Run performs the check. It must honour ctx cancellation and report a
	// timeout as StatusWarn rather than hanging.
	Run(ctx context.Context) CheckResult
}

// Runner executes registered checks, for both the live status bar and the
// one-shot `llmctl doctor` subcommand.
type Runner interface {
	Register(c Check)
	RunAll(ctx context.Context) []CheckResult
}

// DefaultRunner is the standard in-memory implementation of Runner.
type DefaultRunner struct {
	checks []Check
}

// NewRunner creates an empty DefaultRunner ready for registering checks.
func NewRunner() *DefaultRunner {
	return &DefaultRunner{checks: make([]Check, 0)}
}

func (r *DefaultRunner) Register(c Check) {
	if c == nil {
		return
	}
	r.checks = append(r.checks, c)
}

func (r *DefaultRunner) RunAll(ctx context.Context) []CheckResult {
	results := make([]CheckResult, 0, len(r.checks))
	for _, c := range r.checks {
		results = append(results, c.Run(ctx))
	}
	return results
}
