package costestimate

import (
	"unicode/utf8"

	"github.com/DhairyaP4/llmctl/internal/session"
)

// Heuristic is the reference Estimator: one token per four characters, plus a
// fixed allowance per message for the role and formatting a provider wraps
// around it.
//
// # Why not a real tokenizer
//
// There is no single correct tokenizer to use. Counts differ between
// providers and even between models from one provider, and borrowing one, such
// as OpenAI's tiktoken, is not more accurate: it undercounts Claude by roughly
// 15–20% on prose and by more on code. An embedded tokenizer would add a
// dependency and a false sense of precision without making the numbers right.
//
// # Why this is sound for the comparison it serves
//
// The switch screen compares two payloads, and what it reports is their
// ratio. Counted at the same per-character rate, the rate cancels out of that
// ratio exactly, so consistency matters far more than absolute accuracy.
//
// The approximation also errs in the safe direction for coding sessions.
// Code packs more tokens per character than prose, and a full transcript
// replay carries more code than a set of distilled notes. So the real saving
// tends to be larger than the estimate: the tool understates its own result
// rather than overstating it.
//
// Absolute figures are estimates and are labelled as such. The evaluation
// calibrates them against each provider's native count before any number is
// published.
type Heuristic struct{}

const (
	charsPerToken = 4

	// messageOverhead is the allowance for role markers and separators.
	// Counting it means a payload of fewer messages is correctly cheaper, not
	// just a payload of fewer characters.
	messageOverhead = 4
)

// CountText rounds up, so any non-empty text costs at least one token.
func (Heuristic) CountText(text string) int {
	n := utf8.RuneCountInString(text)
	return (n + charsPerToken - 1) / charsPerToken
}

func (h Heuristic) CountMessages(msgs []session.Message) int {
	total := 0
	for _, m := range msgs {
		total += messageOverhead + h.CountText(m.Content)
	}
	return total
}

// CountHandoff counts exactly what a switch would send, through the same
// Payload the send path uses.
func (h Heuristic) CountHandoff(plan session.HandoffPlan) int {
	return h.CountMessages(plan.Payload())
}
