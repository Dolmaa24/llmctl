package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type GPUCheck struct {
	CommandRunner CommandRunner
}

func (GPUCheck) Name() string {
	return "gpu_check"
}

func (c GPUCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "gpu_check", CheckedAt: time.Now()}

	checkCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()

	runCmd := c.CommandRunner
	var outBytes []byte
	var err error

	if runCmd != nil {
		outBytes, err = runCmd(checkCtx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used", "--format=csv,noheader,nounits")
	} else {
		if _, lookErr := exec.LookPath("nvidia-smi"); lookErr != nil {
			result.Status = StatusWarn
			result.Message = "nvidia-smi not found (non-NVIDIA GPU or missing drivers)"
			return result
		}
		cmd := exec.CommandContext(checkCtx, "nvidia-smi", "--query-gpu=name,memory.total,memory.used", "--format=csv,noheader,nounits")
		outBytes, err = cmd.Output()
	}

	if checkCtx.Err() == context.DeadlineExceeded {
		result.Status = StatusWarn
		result.Message = "gpu status check timed out"
		return result
	}

	if err != nil {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("nvidia-smi failed: %v", err)
		return result
	}

	raw := strings.TrimSpace(string(outBytes))
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || lines[0] == "" {
		result.Status = StatusWarn
		result.Message = "nvidia-smi returned empty output"
		return result
	}

	// Example line: "NVIDIA GeForce RTX 4080, 16384, 1200"
	parts := strings.Split(lines[0], ",")
	if len(parts) >= 3 {
		gpuName := strings.TrimSpace(parts[0])
		memTotal := strings.TrimSpace(parts[1])
		memUsed := strings.TrimSpace(parts[2])
		result.Status = StatusOK
		result.Message = fmt.Sprintf("NVIDIA GPU detected: %s (VRAM: %sMiB used / %sMiB total)", gpuName, memUsed, memTotal)
		return result
	}

	result.Status = StatusOK
	result.Message = fmt.Sprintf("NVIDIA GPU detected: %s", lines[0])
	return result
}
