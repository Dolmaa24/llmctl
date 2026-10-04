package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type WSLCheck struct {
	Getenv        func(string) string
	CommandRunner CommandRunner
	GOOS          string
}

func (WSLCheck) Name() string {
	return "wsl_check"
}

func (c WSLCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "wsl_check", CheckedAt: time.Now()}

	getenv := c.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	goos := c.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	// 1. Check if running inside WSL
	distro := getenv("WSL_DISTRO_NAME")
	if distro != "" || getenv("WSL_INTEROP") != "" {
		if distro == "" {
			distro = "Ubuntu"
		}
		result.Status = StatusOK
		result.Message = fmt.Sprintf("WSL2 active inside distro %s", distro)
		return result
	}

	// 2. If non-Windows and not inside WSL
	if goos != "windows" {
		result.Status = StatusWarn
		result.Message = "WSL not detected (non-Windows and not inside WSL)"
		return result
	}

	// 3. On Windows host, check wsl.exe status
	checkCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()

	runCmd := c.CommandRunner
	var outStr string
	if runCmd != nil {
		outBytes, err := runCmd(checkCtx, "wsl.exe", "--status")
		if checkCtx.Err() == context.DeadlineExceeded {
			result.Status = StatusWarn
			result.Message = "wsl status check timed out"
			return result
		}
		if err != nil {
			result.Status = StatusFail
			result.Message = fmt.Sprintf("wsl status failed: %v", err)
			return result
		}
		outStr = string(outBytes)
	} else {
		if _, err := exec.LookPath("wsl.exe"); err != nil {
			result.Status = StatusFail
			result.Message = "wsl.exe not found"
			return result
		}
		cmd := exec.CommandContext(checkCtx, "wsl.exe", "--status")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		if checkCtx.Err() == context.DeadlineExceeded {
			result.Status = StatusWarn
			result.Message = "wsl status check timed out"
			return result
		}
		if err != nil {
			result.Status = StatusFail
			result.Message = fmt.Sprintf("wsl status failed: %v", err)
			return result
		}
		outStr = out.String()
	}

	summary := summarizeWSLStatus(outStr)
	if summary.DefaultVersion == "2" {
		result.Status = StatusOK
		result.Message = fmt.Sprintf("WSL2 ready (default distro: %s)", summary.DefaultDistro)
		return result
	}

	result.Status = StatusWarn
	result.Message = fmt.Sprintf("WSL default version is %s; expected 2", summary.DefaultVersion)
	return result
}

type wslStatusSummary struct {
	DefaultDistro  string
	DefaultVersion string
}

func summarizeWSLStatus(raw string) wslStatusSummary {
	cleaned := strings.ReplaceAll(raw, "\x00", "")
	lines := strings.Split(cleaned, "\n")
	summary := wslStatusSummary{DefaultDistro: "unknown", DefaultVersion: "unknown"}
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		lower := strings.ToLower(l)
		if strings.HasPrefix(lower, "default distribution:") {
			summary.DefaultDistro = strings.TrimSpace(strings.TrimPrefix(l, "Default Distribution:"))
			summary.DefaultDistro = strings.TrimSpace(strings.TrimPrefix(summary.DefaultDistro, "Default distribution:"))
		}
		if strings.HasPrefix(lower, "default version:") {
			summary.DefaultVersion = strings.TrimSpace(strings.TrimPrefix(l, "Default Version:"))
			summary.DefaultVersion = strings.TrimSpace(strings.TrimPrefix(summary.DefaultVersion, "Default version:"))
		}
	}
	return summary
}
