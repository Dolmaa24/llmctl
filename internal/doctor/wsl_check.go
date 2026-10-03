package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type WSLCheck struct{}

func (WSLCheck) Name() string {
	return "wsl_check"
}

func (WSLCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "wsl_check", CheckedAt: time.Now()}
	if runtime.GOOS != "windows" {
		result.Status = StatusWarn
		result.Message = "non-Windows environment; WSL check skipped"
		return result
	}

	if _, err := exec.LookPath("wsl.exe"); err != nil {
		result.Status = StatusFail
		result.Message = "wsl.exe not found"
		return result
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

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

	summary := summarizeWSLStatus(out.String())
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
