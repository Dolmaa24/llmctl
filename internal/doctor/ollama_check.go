package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OllamaCheck struct {
	URL            string
	HTTPClient     HTTPClient
	RequiredModels []string
}

func (OllamaCheck) Name() string {
	return "ollama_check"
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type ollamaPsResponse struct {
	Models []struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		SizeVRAM int64  `json:"size_vram"`
	} `json:"models"`
}

func (c OllamaCheck) Run(ctx context.Context) CheckResult {
	result := CheckResult{Name: "ollama_check", CheckedAt: time.Now()}

	baseURL := c.URL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	checkCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: CheckTimeout}
	}

	// 1. Query /api/tags for pulled models
	tagsReq, err := http.NewRequestWithContext(checkCtx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("invalid ollama url: %v", err)
		return result
	}

	tagsResp, err := client.Do(tagsReq)
	if checkCtx.Err() == context.DeadlineExceeded {
		result.Status = StatusWarn
		result.Message = "ollama check timed out"
		return result
	}
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("ollama server unreachable at %s: %v", baseURL, err)
		return result
	}
	defer tagsResp.Body.Close()

	if tagsResp.StatusCode != http.StatusOK {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("ollama server returned HTTP status %d", tagsResp.StatusCode)
		return result
	}

	body, err := io.ReadAll(tagsResp.Body)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("failed to read ollama response: %v", err)
		return result
	}

	var tagsData ollamaTagsResponse
	if err := json.Unmarshal(body, &tagsData); err != nil {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("ollama server active, but tags response unparseable: %v", err)
		return result
	}

	pulledModels := make([]string, 0, len(tagsData.Models))
	for _, m := range tagsData.Models {
		pulledModels = append(pulledModels, m.Name)
	}

	// 2. Query /api/ps for loaded VRAM models
	var vramModels []string
	psReq, err := http.NewRequestWithContext(checkCtx, http.MethodGet, baseURL+"/api/ps", nil)
	if err == nil {
		psResp, psErr := client.Do(psReq)
		if psErr == nil {
			defer psResp.Body.Close()
			if psResp.StatusCode == http.StatusOK {
				if psBody, psReadErr := io.ReadAll(psResp.Body); psReadErr == nil {
					var psData ollamaPsResponse
					if json.Unmarshal(psBody, &psData) == nil {
						for _, m := range psData.Models {
							if m.SizeVRAM > 0 {
								vramModels = append(vramModels, m.Name)
							}
						}
					}
				}
			}
		}
	}

	// 3. Build summary
	vramInfo := "none"
	if len(vramModels) > 0 {
		vramInfo = strings.Join(vramModels, ", ")
	}

	if len(c.RequiredModels) > 0 {
		missing := []string{}
		for _, req := range c.RequiredModels {
			found := false
			for _, p := range pulledModels {
				if strings.EqualFold(p, req) || strings.HasPrefix(strings.ToLower(p), strings.ToLower(req)+":") {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, req)
			}
		}
		if len(missing) > 0 {
			result.Status = StatusWarn
			result.Message = fmt.Sprintf("ollama active (%d models pulled, VRAM: %s); missing required models: %s",
				len(pulledModels), vramInfo, strings.Join(missing, ", "))
			return result
		}
	}

	result.Status = StatusOK
	result.Message = fmt.Sprintf("ollama active (%d models pulled, VRAM: %s)", len(pulledModels), vramInfo)
	return result
}
