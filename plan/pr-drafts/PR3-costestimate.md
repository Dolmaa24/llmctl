# feat(costestimate): add the missing Estimator interface

## ⚠️ Interface change

This adds a new section to `llmctl_API_INTERFACE_CONTRACT.md`. It is purely
additive — no existing signature changes — but §11 requires acknowledgement
from every consumer of a new shared interface.

**Consumers to tag:**

- **Person 3** — `ui/switchconfirm`, already listed in the contract as a
  consumer of an interface that was never defined
- **Person 2** — owner of `provider.Adapter`, because this PR constrains how
  `Adapter.EstimateTokens` may be used

## The gap

The contract lists `costestimate` as a consumer of `Message` (§2.1) and of
`Adapter` (§3), and names `ui/switchconfirm` as displaying its output (§7). But
no section defines what `costestimate` itself exposes. Person 3 has a modal to
build against an interface that does not exist, and Person 4 owns a package
with no declared surface.

## Proposed interface

```go
package costestimate

type Estimator interface {
    CountText(text string) int
    CountMessages(msgs []session.Message) int
    CountHandoff(plan session.HandoffPlan) int
}
```

## The constraint that matters

`Adapter.EstimateTokens` is per-provider: Anthropic exposes a token-counting
endpoint, Ollama exposes no tokenizer at all, and any fallback is a character
approximation. Counts from two adapters are therefore **not comparable**.

The switch-confirmation modal compares a full replay against a distilled
handoff, and those two payloads may target different providers. If one side is
counted with a real tokenizer and the other with a heuristic, the comparison is
an artifact of the counting method rather than a property of the strategies —
and it is precisely the number this project exists to report.

**So:** one `Estimator` instance counts every side of any comparison.
`Adapter.EstimateTokens` may be used for provider-facing display, never as an
input to a comparison.

This PR adds that constraint as a doc comment on `Adapter.EstimateTokens`,
which is why Person 2 is tagged.

## Deliberately not included

**No pricing table, no currency.** The modal reports tokens only. Prices change
faster than a capstone release cycle, a stale table produces confidently wrong
rupee figures, and for a tool whose premise is catching stale configuration
that is a bad look. Tokens are the stable unit. If the team later wants
currency, it should arrive as a separate `Pricer` interface carrying an
explicit `as_of` date — raise it as its own interface change.

Note this narrows the Phase-I report §3.1 claim that the modal presents "the
exact token count and estimated financial cost". Neither "exact" nor "financial"
is supportable; that sentence needs correcting.

## Requirements addressed

- SRS **FR-3.5** — display estimated token counts for full replay and distilled handoff before a switch
- Plan **TASK-005**

## Contract edits included in this PR

1. Add **§7.1 Cost Estimation** with the `Estimator` interface above.
   **Owner:** Person 4. **Consumed by:** `ui/switchconfirm`, `session/handoff.go`,
   `cmd/llmctl-bench`.

2. Add to §3's contract notes:

   > `EstimateTokens` is provider-specific and its results are not comparable
   > across adapters. Any comparison between two strategies or two providers
   > must be measured with a single `costestimate.Estimator`.

## Checklist (`llmctl_GIT_WORKFLOW.md` §4)

- [x] Targets `main`
- [x] References the requirements it addresses
- [x] CI green
- [x] Contract document updated in this PR
- [ ] Acknowledged by Person 3 (`ui/switchconfirm`)
- [ ] Acknowledged by Person 2 (`provider`)
- [ ] Phase-I report §3.1 correction filed as a follow-up issue
