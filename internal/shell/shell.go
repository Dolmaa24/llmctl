// Package shell detects the host shell and renders environment variables in
// that shell's native syntax. It is one of only two packages permitted to
// contain OS-specific branching (the other is internal/doctor).
//
// # Why this interface is not "Export into the current shell"
//
// The interface contract originally specified an Export method that "writes
// the given env vars into the current shell session". No process can do that:
// a child cannot mutate its parent's environment on Windows or Unix. Anything
// built against that signature would have failed at integration.
//
// Two mechanisms actually work, and both are provided here:
//
//   - Snippet renders assignments the user's shell evaluates itself, via
//     `eval "$(llmctl env)"` in bash or `llmctl env | Invoke-Expression` in
//     PowerShell. The shell performs the mutation on itself.
//   - Launch starts a child process with the variables already in its
//     environment, which is how llmctl runs an agent or tool on the user's
//     behalf.
//
// Writing variables to a shell profile or the Windows registry is deliberately
// not offered: it persists secrets to disk in plaintext and mutates user state
// outside the session.
package shell

import (
	"context"
	"os/exec"
)

// Kind identifies the shell llmctl is running under. Treat it as opaque:
// no module outside this package or internal/doctor may branch on it.
type Kind string

const (
	KindPowerShell Kind = "powershell"
	KindWSLBash    Kind = "wsl_bash"
	KindUnknown    Kind = "unknown"
)

// Detector determines the shell context by inspecting the process ancestry.
type Detector interface {
	Detect() (Kind, error)
}

// Renderer produces shell-native text for a set of environment variables.
type Renderer interface {
	// Snippet renders vars as assignments in the syntax of kind, suitable for
	// the user to evaluate. Values must be quoted and escaped correctly —
	// paths containing spaces, quotes and backslashes are the historical
	// source of bugs here.
	Snippet(kind Kind, vars map[string]string) (string, error)
}

// Launcher runs a child process with vars already present in its environment.
type Launcher interface {
	// Launch starts name with args, inheriting the current environment plus
	// vars, and returns the started command. The caller owns waiting on it.
	Launch(ctx context.Context, vars map[string]string, name string, args ...string) (*exec.Cmd, error)
}

// Syncer reconciles configuration between Windows and WSL so both see the same
// provider setup.
type Syncer interface {
	// Sync reports whether a reconciliation was needed and performed.
	Sync() (synced bool, err error)
}
