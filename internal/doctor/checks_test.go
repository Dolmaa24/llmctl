package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeCheck struct {
	name   string
	status Status
}

func (f fakeCheck) Name() string { return f.name }

func (f fakeCheck) Run(_ context.Context) CheckResult {
	return CheckResult{Name: f.name, Status: f.status}
}

type fakeHTTPClient struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (f fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f.handler(req)
}

func TestRunnerRunAllConcurrent(t *testing.T) {
	r := NewRunner()
	r.Register(fakeCheck{name: "a", status: StatusOK})
	r.Register(fakeCheck{name: "b", status: StatusWarn})

	results := r.RunAll(context.Background())
	if len(results) != 2 {
		t.Fatalf("result count mismatch: got %d", len(results))
	}
	if results[0].Name != "a" || results[1].Name != "b" {
		t.Fatalf("order mismatch: %+v", results)
	}
}

func TestWSLCheckHost(t *testing.T) {
	wsl := WSLCheck{
		GOOS: "windows",
		Getenv: func(key string) string {
			return ""
		},
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("Default Distribution: Ubuntu\nDefault Version: 2\n"), nil
		},
	}

	res := wsl.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "WSL2 ready (default distro: Ubuntu)") {
		t.Fatalf("unexpected message: %s", res.Message)
	}
}

func TestWSLCheckInsideWSL(t *testing.T) {
	wsl := WSLCheck{
		GOOS: "linux",
		Getenv: func(key string) string {
			if key == "WSL_DISTRO_NAME" {
				return "Ubuntu-22.04"
			}
			return ""
		},
	}

	res := wsl.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "WSL2 active inside distro Ubuntu-22.04") {
		t.Fatalf("unexpected message: %s", res.Message)
	}
}

func TestWSLCheckInsideWSL_UnknownDistro(t *testing.T) {
	wsl := WSLCheck{
		GOOS: "linux",
		Getenv: func(key string) string {
			if key == "WSL_INTEROP" {
				return "/run/WSL/1_interop"
			}
			return ""
		},
	}

	res := wsl.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "WSL2 active inside distro unknown") {
		t.Fatalf("unexpected message: %s", res.Message)
	}
}

func TestNetworkCheck(t *testing.T) {
	fakeClient := fakeHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		},
	}

	netCheck := NetworkCheck{
		Endpoints:  []string{"http://127.0.0.1:11434"},
		HTTPClient: fakeClient,
	}

	res := netCheck.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
}

func TestOllamaCheck(t *testing.T) {
	fakeClient := fakeHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/api/tags") {
				body := `{"models":[{"name":"llama3.1:8b"}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
				}, nil
			}
			if strings.HasSuffix(req.URL.Path, "/api/ps") {
				// llama3.1:8b is loaded on GPU (size_vram > 0), cpu_model:7b is loaded on CPU (size_vram == 0)
				body := `{"models":[{"name":"llama3.1:8b","size":4000000000,"size_vram":4000000000},{"name":"cpu_model:7b","size":3500000000,"size_vram":0}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(body)),
				}, nil
			}
			return nil, fmt.Errorf("unknown path: %s", req.URL.Path)
		},
	}

	ollama := OllamaCheck{
		URL:            "http://127.0.0.1:11434",
		HTTPClient:     fakeClient,
		RequiredModels: []string{"llama3.1:8b"},
	}

	res := ollama.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "ollama active (1 models pulled, VRAM: llama3.1:8b)") {
		t.Fatalf("unexpected message: %s", res.Message)
	}
	if strings.Contains(res.Message, "cpu_model:7b") {
		t.Fatalf("expected CPU-only model (size_vram=0) to be excluded from VRAM list, got message: %s", res.Message)
	}
}

func TestGPUCheck(t *testing.T) {
	gpu := GPUCheck{
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("NVIDIA GeForce RTX 4080, 16384, 1200\n"), nil
		},
	}

	res := gpu.Run(context.Background())
	if res.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "NVIDIA GeForce RTX 4080 (VRAM: 1200MiB used / 16384MiB total)") {
		t.Fatalf("unexpected message: %s", res.Message)
	}
}

func TestSummarizeWSLStatus(t *testing.T) {
	raw := "D\x00e\x00f\x00a\x00u\x00l\x00t\x00 \x00D\x00i\x00s\x00t\x00r\x00i\x00b\x00u\x00t\x00i\x00o\x00n\x00:\x00 \x00U\x00b\x00u\x00n\x00t\x00u\x00\nD\x00e\x00f\x00a\x00u\x00l\x00t\x00 \x00V\x00e\x00r\x00s\x00i\x00o\x00n\x00:\x00 \x002\x00\n"
	s := summarizeWSLStatus(raw)
	if s.DefaultDistro != "Ubuntu" {
		t.Fatalf("distro mismatch: %q", s.DefaultDistro)
	}
	if s.DefaultVersion != "2" {
		t.Fatalf("version mismatch: %q", s.DefaultVersion)
	}
}

type panicCheck struct{}

func (panicCheck) Name() string { return "panic_check" }
func (panicCheck) Run(_ context.Context) CheckResult {
	panic("simulated check panic")
}

func TestRunnerPanicRecovery(t *testing.T) {
	r := NewRunner()
	r.Register(fakeCheck{name: "ok_check", status: StatusOK})
	r.Register(panicCheck{})

	results := r.RunAll(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Status != StatusOK {
		t.Fatalf("expected first check StatusOK, got %s", results[0].Status)
	}

	if results[1].Name != "panic_check" {
		t.Fatalf("expected second check name panic_check, got %s", results[1].Name)
	}
	if results[1].Status != StatusFail {
		t.Fatalf("expected panicked check StatusFail, got %s", results[1].Status)
	}
	if !strings.Contains(results[1].Message, "check panicked: simulated check panic") {
		t.Fatalf("unexpected panic message: %s", results[1].Message)
	}
}
