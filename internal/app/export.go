package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/exportform"
	tea "github.com/charmbracelet/bubbletea"
)

// SessionRecord is what the app holds about the current session, in the
// contract's types. It has the same fields as storage.Snapshot on
// feature/sanket-storage, so passing it to storage's exporter is a plain
// conversion.
type SessionRecord struct {
	Session  session.Session
	Messages []session.Message

	// Notes include superseded ones: an export is a record of the session,
	// not just what a handoff would send.
	Notes []session.Note

	// SwitchEvents are recorded by storage, not by the app, so the app leaves
	// this empty. INTEGRATION POINT (P5): load the session from storage by
	// Session.ID to include them.
	SwitchEvents []session.SwitchEvent
}

// ExportFormat names an export file format.
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "markdown"
	ExportJSON     ExportFormat = "json"
)

// SessionExporter renders a session as the bytes of an export file
// (SRS FR-5.3).
//
// Rendering belongs to storage: contract §10 makes P5 the owner of the export
// logic, and the app only collects the session and writes the file. Until the
// storage module is merged, DemoDeps supplies a stand-in. At integration,
// main.go passes a function that calls storage.ExportJSON or
// storage.ExportMarkdown.
type SessionExporter func(f ExportFormat, r SessionRecord, exportedAt time.Time) ([]byte, error)

// exportedMsg carries the outcome of an export back to the update loop. seq
// tells it whether the form on screen is still the one that asked for it.
type exportedMsg struct {
	seq  int
	path string
	err  error
}

// openExport shows the export form, or says why exporting is unavailable.
func (m *Model) openExport() {
	if m.deps.Export == nil {
		m.transcript.SetNotice("exporting is not available in this build")
		return
	}
	m.export.Open(exportStem(m.sessionID, time.Now()), m.exportDir())
}

// exportDir returns the directory a relative export name is written to, as an
// absolute path, so the form can say exactly where the file will go.
func (m Model) exportDir() string {
	dir := m.deps.ExportDir
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// exportStem suggests a file name, without its extension, that is valid on
// every filesystem llmctl runs on: only letters, digits, dashes and
// underscores from the session id, so nothing Windows rejects and nothing
// that could climb out of the directory.
func exportStem(sessionID string, at time.Time) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, sessionID)
	return fmt.Sprintf("llmctl-%s-%s", clean, at.Format("20060102-1504"))
}

// record collects the current session for export. The slices are copied
// because the export runs on another goroutine. The title stays empty until
// storage creates sessions, and the exporters then show it as untitled.
func (m Model) record() SessionRecord {
	active, _ := m.activeProvider()
	return SessionRecord{
		Session: session.Session{
			ID:             m.sessionID,
			ActiveProvider: active.ID,
			ActiveModel:    active.Model,
		},
		Messages: append([]session.Message(nil), m.messages...),
		Notes:    append([]session.Note(nil), m.notes.Notes()...),
	}
}

// submitExport renders the session and writes the file off the update loop
// (SRS NFR-1).
func (m Model) submitExport(s exportform.SubmitMsg) (tea.Model, tea.Cmd) {
	f := ExportMarkdown
	if s.Format == exportform.JSON {
		f = ExportJSON
	}
	path := s.Name
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.exportDir(), path)
	}
	m.export.SetWriting()
	m.exportSeq++
	return m, exportCmd(m.exportSeq, m.deps.Export, f, m.record(), path, time.Now())
}

// exportCmd renders the session and writes it to path. A panic in the
// exporter becomes a failure, for the same reason as in request.
func exportCmd(seq int, export SessionExporter, f ExportFormat, r SessionRecord, path string, at time.Time) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if p := recover(); p != nil {
				msg = exportedMsg{seq: seq, path: path, err: fmt.Errorf("exporter panicked: %v", p)}
			}
		}()
		data, err := export(f, r, at)
		if err != nil {
			return exportedMsg{seq: seq, path: path, err: err}
		}
		return exportedMsg{seq: seq, path: path, err: writeNew(path, data)}
	}
}

// closeExport closes the export form. Bumping exportSeq means an export still
// being written reports through the transcript rather than into a form the
// user has left, or a new one they have opened since.
func (m *Model) closeExport() {
	m.export.Close()
	m.exportSeq++
}

// writeNew writes data to a new file at path.
//
// It never replaces a file that exists: doing so would destroy data without
// the confirmation SRS NFR-8 asks for, so the user is asked for another name
// instead. A write that fails part-way removes the partial file rather than
// leaving half an export behind. The file is readable by its owner only,
// since a transcript holds whatever the user pasted into the conversation.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; choose another name", filepath.Base(path))
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// receiveExport reports a finished export. On failure the form stays open so
// the user can change the name and try again; on success it closes and the
// transcript says where the file went. An export whose form has since been
// closed reports through the transcript either way.
func (m Model) receiveExport(e exportedMsg) (tea.Model, tea.Cmd) {
	current := e.seq == m.exportSeq && m.export.Visible()
	switch {
	case e.err != nil && current:
		m.export.SetFailed(e.err.Error())
	case e.err != nil:
		m.transcript.SetNotice("export failed: " + e.err.Error())
	default:
		if current {
			m.closeExport()
		}
		m.transcript.SetSuccess("exported to " + e.path)
	}
	return m, nil
}
