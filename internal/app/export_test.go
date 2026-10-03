package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/ui/exportform"
)

// From the composer, Tab twice reaches the transcript.
var toTranscript = []string{"tab", "tab"}

func withExportDir(dir string) option {
	return func(d *Deps, _ *State) { d.ExportDir = dir }
}

func withExporter(e SessionExporter) option {
	return func(d *Deps, _ *State) { d.Export = e }
}

// rename moves from the format to the file name, clears the suggestion and
// types name.
func rename(h *harness, name string) {
	h.t.Helper()
	h.press("tab", "ctrl+u")
	h.typeText(name)
}

func TestExportWritesTheSession(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 32, withExportDir(dir))
	h.press(append(toTranscript, "x")...)
	if !strings.Contains(h.view(), "Export session") {
		t.Fatalf("the export form did not open:\n%s", h.view())
	}
	rename(h, "session.md")
	h.press("enter")

	path := filepath.Join(dir, "session.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no file written: %v", err)
	}
	for _, want := range []string{"The refill timer is dropping ticks", "Decided: atomic CAS"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the export is missing %q:\n%s", want, data)
		}
	}
	if h.model().export.Visible() {
		t.Error("the form is still open after a successful export")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, path) {
		t.Errorf("notice = %q, want the file's path", n)
	}
	// A success must not look like an error, or the user will doubt it.
	if out := h.view(); !strings.Contains(out, "✓ exported to") || strings.Contains(out, "! exported") {
		t.Errorf("the success is not shown as one:\n%s", out)
	}
}

func TestExportCanWriteJSON(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 32, withExportDir(dir))
	h.press(append(toTranscript, "x", "right")...)
	if f := h.model().export.Format(); f != exportform.JSON {
		t.Fatalf("format = %v, want JSON", f)
	}
	h.press("enter") // the suggested name

	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("JSON files written: %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !json.Valid(data) {
		t.Errorf("not valid JSON:\n%s", data)
	}
}

// The suggested name's extension follows the format; a name the user gave
// its own extension keeps it.
func TestExtensionFollowsTheFormat(t *testing.T) {
	h := newHarness(t, 120, 32, withExportDir(t.TempDir()))
	h.press(append(toTranscript, "x")...)
	if n := h.model().export.Name(); !strings.HasPrefix(n, "llmctl-demo-") || !strings.HasSuffix(n, ".md") {
		t.Fatalf("suggested %q", n)
	}
	h.press("right")
	if n := h.model().export.Name(); !strings.HasSuffix(n, ".json") {
		t.Errorf("after choosing JSON the name is %q", n)
	}
	h.press("left")
	if n := h.model().export.Name(); !strings.HasSuffix(n, ".md") {
		t.Errorf("after choosing Markdown again the name is %q", n)
	}

	rename(h, "notes.txt")
	h.press("shift+tab", "right")
	if n := h.model().export.Name(); n != "notes.txt" {
		t.Errorf("a name the user chose became %q", n)
	}
}

// An existing file is never replaced: that would destroy data without the
// confirmation SRS NFR-8 asks for. The user picks another name instead.
func TestExportNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.md")
	if err := os.WriteFile(path, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, 120, 32, withExportDir(dir))
	h.press(append(toTranscript, "x")...)
	rename(h, "keep.md")
	h.press("enter")

	if data, _ := os.ReadFile(path); string(data) != "precious" {
		t.Error("the existing file was replaced")
	}
	if !h.model().export.Visible() {
		t.Fatal("the form closed, leaving no way to pick another name")
	}
	if !strings.Contains(h.view(), "keep.md already exists") {
		t.Errorf("the form does not say why:\n%s", h.view())
	}

	h.press("ctrl+u")
	h.typeText("other.md")
	h.press("enter")
	if _, err := os.Stat(filepath.Join(dir, "other.md")); err != nil {
		t.Errorf("a new name did not export: %v", err)
	}
}

// The exporter receives the whole session: every message, every note
// including superseded ones, and which session it is.
func TestExportSendsTheWholeSession(t *testing.T) {
	var (
		got    SessionRecord
		format ExportFormat
		at     time.Time
	)
	spy := func(f ExportFormat, r SessionRecord, when time.Time) ([]byte, error) {
		got, format, at = r, f, when
		return []byte("exported"), nil
	}
	h := newHarness(t, 120, 32, withExportDir(t.TempDir()), withExporter(spy))
	h.press(append(toTranscript, "x", "right", "enter")...)

	if format != ExportJSON {
		t.Errorf("format = %q, want json", format)
	}
	if got.Session.ID != "demo" || got.Session.ActiveProvider != "ollama" {
		t.Errorf("session = %+v", got.Session)
	}
	if len(got.Messages) != len(h.model().messages) {
		t.Errorf("exported %d of %d messages", len(got.Messages), len(h.model().messages))
	}
	superseded := false
	for _, n := range got.Notes {
		superseded = superseded || n.ID == "n-003"
	}
	if !superseded {
		t.Error("the superseded note was left out; an export is a record of the session")
	}
	if at.IsZero() {
		t.Error("no export time given")
	}
}

// A failed export keeps the form open with the reason, and writes nothing.
func TestExportFailureKeepsTheForm(t *testing.T) {
	for name, exporter := range map[string]SessionExporter{
		"error": func(ExportFormat, SessionRecord, time.Time) ([]byte, error) {
			return nil, errors.New("renderer unavailable")
		},
		"panic": func(ExportFormat, SessionRecord, time.Time) ([]byte, error) {
			panic("renderer unavailable")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			h := newHarness(t, 120, 32, withExportDir(dir), withExporter(exporter))
			h.press(append(toTranscript, "x", "enter")...)

			if !h.model().export.Visible() {
				t.Fatal("the form closed on failure")
			}
			if !strings.Contains(h.view(), "renderer unavailable") {
				t.Errorf("the form does not say why:\n%s", h.view())
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("a failed export wrote %d files", len(entries))
			}
		})
	}
}

// An export still being written when its form closes reports through the
// transcript, and never into a form opened since.
func TestAnExportFinishingLateReportsInTheTranscript(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 32, withExportDir(dir))
	h.press(append(toTranscript, "x")...)
	rename(h, "late.md")
	h.hold("enter")     // the write is parked, in flight
	h.press("esc", "x") // close that form, open a new one
	h.release()

	if !h.model().export.Visible() {
		t.Error("the earlier export's result closed the new form")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "exported to") {
		t.Errorf("notice = %q, want the late export reported", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "late.md")); err != nil {
		t.Errorf("the late export was not written: %v", err)
	}
}

func TestEscClosesExportWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, 120, 32, withExportDir(dir))
	h.press(append(toTranscript, "x", "esc")...)

	if h.model().export.Visible() {
		t.Error("esc left the form open")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("cancelling wrote %d files", len(entries))
	}
}

// While the composer has focus, x is text.
func TestXIsTextInTheComposer(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("x")
	if h.model().export.Visible() {
		t.Error("x opened the export form from the composer")
	}
	if v := h.model().composer.Value(); v != "x" {
		t.Errorf("composer holds %q", v)
	}
}

func TestExportOpensFromTheNotesPane(t *testing.T) {
	h := newHarness(t, 120, 32, withExportDir(t.TempDir()))
	h.press("tab", "tab", "tab", "x")
	if !h.model().export.Visible() {
		t.Error("x did not open the export form from the notes pane")
	}
}

func TestExportIsOfferedOnlyWhenAvailable(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press(toTranscript...)
	if !strings.Contains(h.view(), "x export") {
		t.Error("the help bar does not offer export")
	}

	h = newHarness(t, 120, 32, withExporter(nil))
	h.press(append(toTranscript, "x")...)
	if strings.Contains(h.view(), "x export") {
		t.Error("export is offered with no exporter")
	}
	if h.model().export.Visible() {
		t.Error("the form opened with no exporter")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "not available") {
		t.Errorf("notice = %q, want why export is unavailable", n)
	}
}

// The suggested name holds nothing Windows rejects and nothing that could
// climb out of the folder, whatever the session id is.
func TestExportStemIsSafe(t *testing.T) {
	at := time.Date(2026, 10, 4, 15, 30, 0, 0, time.UTC)
	got := exportStem(`../a:b\c*d?"e<f>|g`, at)
	if strings.ContainsAny(got, `/\:*?"<>|.`) {
		t.Errorf("unsafe stem %q", got)
	}
	if !strings.HasPrefix(got, "llmctl-") || !strings.HasSuffix(got, "-20261004-1530") {
		t.Errorf("stem = %q", got)
	}
}
