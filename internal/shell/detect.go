package shell

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type SystemDetector struct {
	Getenv               func(string) string
	GOOS                 string
	GetParentProcessName func() (string, error)
}

func (d SystemDetector) Detect() (Kind, error) {
	goos := d.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	getParent := d.GetParentProcessName
	if getParent == nil {
		getParent = defaultGetParentProcessName
	}

	return detectShell(goos, getenv, getParent)
}

func detectShell(goos string, getenv func(string) string, getParent func() (string, error)) (Kind, error) {
	switch goos {
	case "windows":
		if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
			return KindWSLBash, nil
		}
		if getParent != nil {
			parent, err := getParent()
			if err == nil && parent != "" {
				pLower := strings.ToLower(parent)
				if pLower == "cmd.exe" || strings.HasSuffix(pLower, "\\cmd.exe") {
					return "", fmt.Errorf("llmctl must be run from PowerShell (cmd.exe is not supported)")
				}
			}
		}
		return KindPowerShell, nil
	case "linux":
		if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
			return KindWSLBash, nil
		}
		return "", fmt.Errorf("unsupported linux environment: llmctl requires WSL2")
	default:
		return "", fmt.Errorf("unsupported operating system: %s", goos)
	}
}

func defaultGetParentProcessName() (string, error) {
	ppid := os.Getppid()
	if ppid <= 0 {
		return "", fmt.Errorf("invalid ppid")
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", ppid), "/FO", "CSV", "/NH")
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			parts := strings.Split(lines[0], ",")
			if len(parts) > 0 {
				name := strings.Trim(strings.TrimSpace(parts[0]), "\"")
				return strings.ToLower(name), nil
			}
		}
		return "", fmt.Errorf("parent process not found")
	}
	return "", nil
}
