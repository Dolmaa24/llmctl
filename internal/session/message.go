// Package session holds the canonical data types shared across every llmctl
// module. Per llmctl_API_INTERFACE_CONTRACT.md section 2, these types must not
// be redefined elsewhere: storage persists them, provider adapters consume
// them, and the UI renders them.
package session

import "time"

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is one turn in a session transcript, attributed to the provider and
// model that produced it. Provider and Model are empty for user messages.
type Message struct {
	ID          string // UUID
	SessionID   string
	SequenceNum int // order within the session; do not rely on CreatedAt alone
	Role        Role
	Content     string
	Provider    string
	Model       string
	TokenCount  int // 0 if not yet estimated
	CreatedAt   time.Time
}
