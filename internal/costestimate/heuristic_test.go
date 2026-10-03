package costestimate_test

import (
	"strings"
	"testing"

	"github.com/DhairyaP4/llmctl/internal/costestimate"
	"github.com/DhairyaP4/llmctl/internal/session"
)

var h costestimate.Heuristic

func TestCountTextRoundsUp(t *testing.T) {
	for text, want := range map[string]int{
		"":      0,
		"a":     1,
		"abcd":  1,
		"abcde": 2,
		"héllo": 2, // five characters, not six bytes
	} {
		if got := h.CountText(text); got != want {
			t.Errorf("CountText(%q) = %d, want %d", text, got, want)
		}
	}
}

// Fewer messages must be cheaper even at equal length, because each message
// carries role and formatting tokens. Without the overhead, merging turns
// would look free.
func TestEachMessageCarriesOverhead(t *testing.T) {
	one := []session.Message{{Content: "abcdefgh"}}
	two := []session.Message{{Content: "abcd"}, {Content: "efgh"}}
	if h.CountMessages(two) <= h.CountMessages(one) {
		t.Errorf("two messages (%d) should cost more than one of the same length (%d)",
			h.CountMessages(two), h.CountMessages(one))
	}
}

// The comparison reports a ratio, and at a fixed per-character rate that rate
// cancels out. So scaling both payloads by the same factor leaves the ratio
// almost unchanged; only rounding and per-message overhead move it.
func TestRatioIsStableUnderScaling(t *testing.T) {
	replay := []session.Message{{Content: strings.Repeat("x", 4000)}}
	distilled := []session.Message{{Content: strings.Repeat("x", 800)}}
	r1 := float64(h.CountMessages(distilled)) / float64(h.CountMessages(replay))

	replay[0].Content = strings.Repeat("x", 40000)
	distilled[0].Content = strings.Repeat("x", 8000)
	r2 := float64(h.CountMessages(distilled)) / float64(h.CountMessages(replay))

	if diff := r1 - r2; diff > 0.01 || diff < -0.01 {
		t.Errorf("ratio moved from %.4f to %.4f under uniform scaling", r1, r2)
	}
}

// CountHandoff must count exactly what a switch sends.
func TestCountHandoffCountsThePayload(t *testing.T) {
	plan := session.HandoffPlan{
		Notes:          []session.Note{{Content: "decided: CAS", Tags: []string{"decision"}}},
		RecentRawTurns: []session.Message{{Role: session.RoleUser, Content: "and now?"}},
	}
	if got, want := h.CountHandoff(plan), h.CountMessages(plan.Payload()); got != want {
		t.Errorf("CountHandoff = %d, CountMessages(Payload) = %d", got, want)
	}
}
