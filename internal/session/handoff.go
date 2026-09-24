package session

import "context"

// HandoffPlan is what will actually be sent to the new provider on a switch:
// the accumulated non-superseded notes plus the last N raw turns. Building it
// sends nothing — it is a pure planning step so the UI can show the cost
// comparison before the user confirms.
type HandoffPlan struct {
	Notes          []Note
	RecentRawTurns []Message

	EstimatedTokensFullReplay int
	EstimatedTokensDistilled  int
}

// HandoffBuilder assembles a HandoffPlan for a session.
type HandoffBuilder interface {
	BuildHandoff(ctx context.Context, sess *Session) (HandoffPlan, error)
}
