package notes

// Target is where extraction runs: a provider and one of its models.
type Target struct {
	Provider string
	Model    string
}

// cheapModels maps a hosted provider to the cheapest of its models that
// extracts well.
//
// Extraction reads every stretch of the conversation whether or not the user
// ever switches, so it is paid for far more often than any handoff. That cost
// is what distillation has to earn back, so extraction runs on a cheap model
// rather than on whichever model the conversation is using. Both entries are
// Claude Haiku 4.5, at $1 per million input tokens and $5 per million output
// tokens by either route (prices and identifiers checked 27 September 2026).
// Identifiers are pinned versions rather than aliases such as "latest", so a
// result can be reproduced (REQ-006).
var cheapModels = map[string]string{
	"anthropic":  "claude-haiku-4-5-20251001",
	"openrouter": "anthropic/claude-haiku-4.5",
	// ollama: placeholder until go test -tags live picks the smallest model
	// that retains nearly all planted facts on Sanket's laptop (TASK-022).
	"ollama": "llama3.1:8b",
}

// CheapTarget chooses where to take notes on a conversation that has been
// running on conversation: the cheapest good model on that same provider.
// Pass the provider the new turns were sent to, which at a switch is the one
// being left, not the one being switched to.
//
// Staying on the conversation's provider means extraction never sends the
// conversation anywhere the user has not already sent it. Without this rule,
// merely having an Anthropic key configured would send every exchange of a
// conversation held on a local model to Anthropic to be summarised.
//
// A local provider currently has a placeholder entry (see cheapModels above).
// Once the live test on the laptop picks a specific model, the placeholder will
// be replaced with the exact tag and quantization. An unlisted provider falls
// back to the conversation's own model.
func CheapTarget(conversation Target) Target {
	if model, ok := cheapModels[conversation.Provider]; ok {
		return Target{Provider: conversation.Provider, Model: model}
	}
	return conversation
}
