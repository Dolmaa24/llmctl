package doctor

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type NetworkCheck struct {
	Endpoints  []string
	HTTPClient HTTPClient
}

func (NetworkCheck) Name() string {
	return "network_check"
}

func (c NetworkCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "network_check", CheckedAt: time.Now()}

	endpoints := c.Endpoints
	if len(endpoints) == 0 {
		endpoints = []string{
			"http://127.0.0.1:11434",
			"https://api.anthropic.com",
			"https://openrouter.ai/api",
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: CheckTimeout}
	}

	var reachable []string
	var failed []string

	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, ep, nil)
		if err != nil {
			failed = append(failed, ep)
			continue
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				reachable = append(reachable, ep)
				continue
			}
		}

		// Fallback to GET if HEAD was not accepted (e.g. 405 Method Not Allowed)
		getReq, err := http.NewRequestWithContext(checkCtx, http.MethodGet, ep, nil)
		if err == nil {
			getResp, getErr := client.Do(getReq)
			if getErr == nil {
				getResp.Body.Close()
				if getResp.StatusCode >= 200 && getResp.StatusCode < 500 {
					reachable = append(reachable, ep)
					continue
				}
			}
		}

		failed = append(failed, ep)
	}

	if checkCtx.Err() == context.DeadlineExceeded {
		result.Status = StatusWarn
		result.Message = "network check timed out"
		return result
	}

	if len(reachable) > 0 {
		result.Status = StatusOK
		result.Message = fmt.Sprintf("provider endpoints reachable (%s)", strings.Join(reachable, ", "))
		return result
	}

	result.Status = StatusWarn
	result.Message = fmt.Sprintf("no provider endpoints reachable (failed: %s)", strings.Join(failed, ", "))
	return result
}
