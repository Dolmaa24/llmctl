package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var validVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SystemRenderer renders environment variables in the syntax of the target shell.
type SystemRenderer struct{}

func (SystemRenderer) Snippet(kind Kind, vars map[string]string) (string, error) {
	if len(vars) == 0 {
		return "", nil
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		if !validVarName.MatchString(k) {
			return "", fmt.Errorf("invalid environment variable name %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		v := vars[k]
		switch kind {
		case KindPowerShell:
			if _, err := fmt.Fprintf(&sb, "$env:%s='%s'\n", k, escapePowerShell(v)); err != nil {
				return "", fmt.Errorf("write powershell export: %w", err)
			}
		case KindWSLBash:
			if _, err := fmt.Fprintf(&sb, "export %s='%s'\n", k, escapeBash(v)); err != nil {
				return "", fmt.Errorf("write wsl export: %w", err)
			}
		default:
			return "", fmt.Errorf("unsupported shell kind: %s", kind)
		}
	}

	return sb.String(), nil
}

// SystemLauncher launches a child process with the given variables set.
type SystemLauncher struct{}

func (SystemLauncher) Launch(ctx context.Context, vars map[string]string, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	for k, v := range vars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch command %s: %w", name, err)
	}
	return cmd, nil
}

// SystemSyncer synchronizes configurations between Windows and WSL.
type SystemSyncer struct{}

func (SystemSyncer) Sync() (bool, error) {
	// Phase 1 leaves cross-filesystem sync to the integration flow.
	return false, nil
}

func escapePowerShell(value string) string {
	r := strings.NewReplacer(
		"'", "''",
		"‘", "‘‘",
		"’", "’’",
		"‚", "‚‚",
		"‛", "‛‛",
	)
	return r.Replace(value)
}

func escapeBash(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}
