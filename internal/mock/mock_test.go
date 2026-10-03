package mock

import (
	"testing"

	"github.com/DhairyaP4/llmctl/internal/config"
	"github.com/DhairyaP4/llmctl/internal/doctor"
	"github.com/DhairyaP4/llmctl/internal/notes"
	"github.com/DhairyaP4/llmctl/internal/provider"
	"github.com/DhairyaP4/llmctl/internal/shell"
	"github.com/DhairyaP4/llmctl/internal/storage"
)

// TestMocksSatisfyContract fails to compile if any mock drifts from the
// interface it stands in for. That compile error is the point of this test:
// it is the cheapest possible guard on the interface contract.
func TestMocksSatisfyContract(t *testing.T) {
	var (
		_ provider.Adapter         = (*Adapter)(nil)
		_ storage.SessionsRepo     = (*SessionsRepo)(nil)
		_ storage.MessagesRepo     = (*MessagesRepo)(nil)
		_ storage.NotesRepo        = (*NotesRepo)(nil)
		_ storage.SwitchEventsRepo = (*SwitchEventsRepo)(nil)
		_ shell.Detector           = (*Detector)(nil)
		_ shell.Renderer           = (*Renderer)(nil)
		_ shell.Launcher           = (*Launcher)(nil)
		_ doctor.Check             = (*Check)(nil)
		_ notes.Extractor          = (*Extractor)(nil)
		_ storage.ExtractionRunsRepo             = (*ExtractionRunsRepo)(nil)
		_ config.Store             = (*ConfigStore)(nil)
		_ config.SecretStore       = (*SecretStore)(nil)
	)
}
