package session

import "time"

// Session is the top-level record for one working session in the TUI.
// Messages, notes and switch events all hang off it.
//
// This type is referenced by storage.SessionsRepo and HandoffBuilder but was
// never defined in llmctl_API_INTERFACE_CONTRACT.md section 2; it is added
// here and to the contract as section 2.4 (TASK-003).
type Session struct {
	ID             string // UUID
	Title          string // user-editable, or generated from the first message
	ActiveProvider string
	ActiveModel    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
