package doctor

import (
	"context"
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

func TestRunnerRunAll(t *testing.T) {
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
