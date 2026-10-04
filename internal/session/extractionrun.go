package session

import "time"

// ExtractionRun records one note-extraction pass: how far it read the
// transcript, which model and prompt did the reading, and what it cost.
//
// This is the extraction side of C_total = C_handoff + C_extraction that
// the switch screen shows. It also marks where the next pass starts: the
// next extraction reads only messages after the furthest Through of any
// completed run, so this record must survive restarts.
type ExtractionRun struct {
	ID        string // assigned by storage
	SessionID string

	// ThroughSequenceNum is the SequenceNum of the last message the pass
	// read. The next pass starts after the furthest one of any completed run.
	ThroughSequenceNum int

	// The extraction model — not the conversation's model.
	Provider      string
	Model         string
	PromptVersion string

	InputTokens  int
	OutputTokens int

	NoteIDs []string // IDs of notes the pass stored

	// Err is empty for a completed pass. A failed pass moves the read point
	// nowhere but is stored — it was paid for, so it counts toward C_total.
	Err string

	CreatedAt time.Time
}
