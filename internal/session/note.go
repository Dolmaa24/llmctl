package session

import "time"

// Note is a single distilled fact extracted from the conversation — a
// decision, constraint, established fact or open question — stored as its own
// row rather than as part of a summary paragraph.
//
// The contract placed this type in internal/notes/ in prose while every
// signature wrote it as session.Note. It is canonical here (TASK-002); the
// notes package imports session, never the reverse.
type Note struct {
	ID        string // UUID
	SessionID string
	Content   string // the distilled fact, e.g. "decided: atomic CAS for refill timer"
	Provider  string // which provider's turn this was extracted from
	Model     string
	Tags      []string // e.g. ["decision", "concurrency"]

	// LinksTo and SupersededBy are self-referencing note IDs.
	//
	// Populate them wherever extraction can determine them, even though
	// nothing in this project's scope reads them for ranking. Per PRD
	// section 8 this is what keeps a future retrieval upgrade additive
	// rather than a rewrite — leaving them permanently nil is the one
	// shortcut that must not be taken.
	LinksTo      *string
	SupersededBy *string

	CreatedAt time.Time
}

// Active reports whether the note should be included in a handoff. Superseded
// notes are excluded so a replaced decision is not carried to a new provider.
func (n Note) Active() bool { return n.SupersededBy == nil }
