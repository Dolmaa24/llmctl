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

// NewSwitchEvent records a switch that sent plan to toProvider/toModel. The
// counts and estimates come from the plan itself, so the log shows what was
// actually sent rather than what was intended.
//
// ID is left for storage to assign, as for messages.
func NewSwitchEvent(sess *Session, plan HandoffPlan, toProvider, toModel string, at time.Time) SwitchEvent {
	return SwitchEvent{
		SessionID:                 sess.ID,
		FromProvider:              sess.ActiveProvider,
		FromModel:                 sess.ActiveModel,
		ToProvider:                toProvider,
		ToModel:                   toModel,
		NotesSentCount:            len(plan.Notes),
		RawTurnsSentCount:         len(plan.RecentRawTurns),
		EstimatedTokensFullReplay: plan.EstimatedTokensFullReplay,
		EstimatedTokensDistilled:  plan.EstimatedTokensDistilled,
		CreatedAt:                 at,
	}
}
