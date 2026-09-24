// Package costestimate counts tokens for the pre-switch comparison.
//
// This interface was missing from llmctl_API_INTERFACE_CONTRACT.md:
// ui/switchconfirm was listed as a consumer with no owner-side interface
// defined (TASK-005).
package costestimate

import "github.com/team260/llmctl/internal/session"

// Estimator counts tokens consistently across providers.
//
// A single Estimator must be used for every arm of any comparison. Provider
// tokenizers differ, so measuring one strategy with Anthropic's counter and
// another with a character-based approximation produces a number that looks
// like a result and is not one.
type Estimator interface {
	// CountText returns the token count for a single string.
	CountText(text string) int

	// CountMessages returns the token count for an assembled conversation.
	CountMessages(msgs []session.Message) int

	// CountHandoff returns the token count for a planned handoff payload.
	CountHandoff(plan session.HandoffPlan) int
}
