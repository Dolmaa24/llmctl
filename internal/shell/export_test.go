package shell

import (
	"bytes"
	"strings"
	"testing"
)

func TestSystemExporterExport(t *testing.T) {
	var b bytes.Buffer
	exported := map[string]string{}
	exp := SystemExporter{
		Writer: &b,
		Setenv: func(k, v string) error {
			exported[k] = v
			return nil
		},
	}

	vars := map[string]string{
		"ANTHROPIC_API_KEY": "a'b",
	}

	if err := exp.Export(KindPowerShell, vars); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	if exported["ANTHROPIC_API_KEY"] != "a'b" {
		t.Fatalf("setenv mismatch")
	}
	out := b.String()
	if !strings.Contains(out, "$env:ANTHROPIC_API_KEY='a''b'") {
		t.Fatalf("unexpected powershell output: %s", out)
	}
}

func TestSystemRendererSnippet(t *testing.T) {
	r := SystemRenderer{}
	vars := map[string]string{
		"KEY": "val'ue",
	}

	psSnippet, err := r.Snippet(KindPowerShell, vars)
	if err != nil {
		t.Fatalf("snippet ps failed: %v", err)
	}
	if !strings.Contains(psSnippet, "$env:KEY='val''ue'") {
		t.Fatalf("unexpected ps snippet: %s", psSnippet)
	}

	bashSnippet, err := r.Snippet(KindWSLBash, vars)
	if err != nil {
		t.Fatalf("snippet bash failed: %v", err)
	}
	if !strings.Contains(bashSnippet, "export KEY='val'\"'\"'ue'") {
		t.Fatalf("unexpected bash snippet: %s", bashSnippet)
	}
}
