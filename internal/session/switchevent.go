package session

import "time"

// SwitchEvent records one provider/model switch and the cost tradeoff shown to
// the user, so every switch leaves evidence of what was sent and what the
// naive alternative would have cost.
type SwitchEvent struct {
	ID        string
	SessionID string

	FromProvider string
	FromModel    string
	ToProvider   string
	ToModel      string

	NotesSentCount    int
	RawTurnsSentCount int

	EstimatedTokensFullReplay int
	EstimatedTokensDistilled  int

	CreatedAt time.Time
}
