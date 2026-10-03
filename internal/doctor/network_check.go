package doctor

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type NetworkCheck struct {
	URL string
}

func (NetworkCheck) Name() string {
	return "network_check"
}

func (c NetworkCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "network_check", CheckedAt: time.Now()}
	target := c.URL
	if target == "" {
		target = "https://api.github.com"
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, target, nil)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("invalid network target: %v", err)
		return result
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if checkCtx.Err() == context.DeadlineExceeded {
		result.Status = StatusWarn
		result.Message = "network check timed out"
		return result
	}
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("network unavailable: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		result.Status = StatusOK
		result.Message = fmt.Sprintf("network reachable (%d)", resp.StatusCode)
		return result
	}

	result.Status = StatusWarn
	result.Message = fmt.Sprintf("network reachable but returned status %d", resp.StatusCode)
	return result
}
