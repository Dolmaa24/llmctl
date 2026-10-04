package shell

import (
	"strings"
	"testing"
)

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

func TestSystemRendererSnippetCurlyQuotes(t *testing.T) {
	r := SystemRenderer{}
	vars := map[string]string{
		"QUOTES": "‘smart’ ‚quotes‛ and 'single'",
	}

	psSnippet, err := r.Snippet(KindPowerShell, vars)
	if err != nil {
		t.Fatalf("snippet ps failed: %v", err)
	}
	expected := "$env:QUOTES='‘‘smart’’ ‚‚quotes‛‛ and ''single'''"
	if !strings.Contains(psSnippet, expected) {
		t.Fatalf("unexpected curly quotes snippet: got %q, expected substring %q", psSnippet, expected)
	}
}

func TestSystemRendererSnippetInvalidVarName(t *testing.T) {
	r := SystemRenderer{}
	invalidVars := []map[string]string{
		{"123INVALID": "val"},
		{"KEY-WITH-DASH": "val"},
		{"KEY$DOLLAR": "val"},
		{"KEY WITH SPACE": "val"},
	}

	for _, vars := range invalidVars {
		_, err := r.Snippet(KindPowerShell, vars)
		if err == nil {
			t.Fatalf("expected error for invalid var name %+v, got nil", vars)
		}
	}
}
