# ADR-0007: LLM isolated to prose generation, never decisions

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Backend

## Context

Optiqor uses an LLM (currently Anthropic's Claude, with a planned OpenAI fallback) for one specific purpose: turning structured technical findings into human-readable prose for PR comments and dashboards.

The architectural risk is significant. If the LLM's output influences *what* Optiqor decides — what value to recommend, what fix to apply, what severity to assign — then:

1. **Decisions become non-deterministic.** Two identical inputs can produce different recommendations. The methodology stops being verifiable.
2. **The receipt trust property collapses.** A signed receipt for "we recommended 400m CPU" is only meaningful if the math, not an LLM, produced 400m.
3. **The "LLM wrapper" criticism becomes literally true.** Investors and customers will ask "what happens when GPT-5 ships?" — if the LLM is in the decision path, the answer is "we get better." If the LLM is only in the prose path, the answer is "nothing changes about what Optiqor decides; the explanations get marginally better."
4. **Prompt injection becomes catastrophic.** A malicious manifest could nudge the LLM into recommending wrong values. With the LLM out of the decision path, the worst case is wrong prose.

This is also a positioning question, addressed in detail in the strategy docs. Architecturally, the question is: how do we enforce the isolation in code, not just in claims?

## Decision

**The LLM produces prose, not values. Values come from the methodology library.** This is enforced by code structure:

1. The methodology library (`internal/methodology/`, per ADR-0006) has **no dependency on any LLM client**. Lint rule: `internal/methodology/` cannot import `internal/explain/` or any LLM SDK. CI fails if violated.
2. All LLM calls live in `internal/explain/`, a separate package whose **only outputs are strings** (prose for PR descriptions, inline comments, dashboard explanations). The package signature does not include numeric values.
3. The orchestration layer first calls methodology to compute decisions (numeric, structured, deterministic), then calls explain to generate prose. The prose is *additional* to the decision; if it fails, the decision still stands.
4. Every LLM call is logged to `llm_calls` with input hash, output hash, provider, model, cost. Customer-visible explanations carry a footer link to verify the underlying numbers against the methodology spec.
5. The LLM client is abstracted behind a provider-agnostic interface; Anthropic and OpenAI are interchangeable implementations. No code outside `internal/explain/` knows which provider was used.
6. Output validation: prose is checked for length bounds, profanity (commercial filter), and that it does not contain numeric claims that could be mistaken for methodology output. If the LLM tries to insert "$5,000 savings" into the prose, validation strips it and substitutes the methodology-computed value.

If the LLM is unreachable, the system continues to function: the recommendation, the PR, the receipt all proceed. Only the prose degrades to a template-generated summary. **The product is never blocked on the LLM.**

**Implementation status (2026-05-18):** the LLM code currently lives at `internal/agent/llm/` for historical reasons — the `internal/agent/` package was originally named for "LLM agent" before the in-cluster K8s `cmd/agent` was added, creating a naming collision. The architectural rules in this ADR apply to the current location and to the eventual rename. **Physical rename of `internal/agent/llm/` → `internal/explain/` is deferred** until the next material refactor of that package; it's a clarity win, not a correctness fix. Until then, treat `internal/agent/llm/` as the canonical location for the rules above.

## Alternatives considered

**Alternative 1: LLM as decision-maker, methodology as advisor.**
Pass methodology output to the LLM as context; let the LLM produce the final recommendation. Rejected because: destroys determinism, destroys the receipt trust property, exposes Optiqor to prompt injection in customer manifests, makes "we're not an LLM wrapper" untrue. Non-starter.

**Alternative 2: No LLM at all; all prose from templates.**
Simpler, fully deterministic. Rejected because: template prose at scale is robotic. The PR description is part of the user experience; customers consistently rate well-written explanations highly. The LLM is genuine value-add for prose quality. The cost-benefit tilts toward "use it, but isolated."

**Alternative 3: LLM produces structured suggestions that methodology validates.**
A middle ground: LLM proposes a recommended value, methodology validates whether it's reasonable, accepts or overrides. Rejected because: this is still letting the LLM into the decision path, just with a validation step. Determinism is still broken. The validation step itself becomes the methodology's actual logic, making the LLM redundant.

**Alternative 4: Multiple LLM providers ensemble for higher quality.**
Call both Anthropic and OpenAI, pick the better answer. Rejected for prose because: marginal quality gain for double the cost and complexity. Stick with one primary, one fallback.

## Consequences

**Easier:**
- The methodology can be tested without any LLM mocking. Pure function in, pure struct out.
- LLM provider outages are graceful: the product continues working with template-generated prose. No P1 incidents from Anthropic outages.
- Swapping providers (Anthropic → OpenAI or vice versa) is a config flag. No customer-visible behavior change.
- The "we're not an LLM wrapper" claim is structurally true and auditable. An engineer or external reviewer can verify it by reading the imports.

**Harder:**
- Two-stage call pattern (methodology, then explain) is slightly more code than a single combined call. Acceptable cost.
- Prose templates must exist as fallbacks for every explanation type. **Mitigation: write the templates first, layer LLM enhancement on top, so the templates are the source of truth and the LLM merely improves them.**
- LLM cost monitoring is critical to keep margins healthy. **Target: LLM cost ≤ 8% of revenue.** Prompt caching, response caching, and short prompts all required.

**Locked into:**
- Two specific isolation rules: (1) `internal/methodology/` has no LLM imports, (2) `internal/explain/` (currently `internal/agent/llm/`) returns strings only. These rules are checked in CI; weakening them requires a new ADR.
- The LLM is forever optional. Even when it's working well, the product must function without it. This constrains us against ever building features that *require* the LLM (e.g., a chat interface that wouldn't degrade gracefully).

**When we'd revisit this:**
- Never for the core principle (LLM out of decisions). The trust property depends on it.
- For specific *additional* uses of LLMs, on a case-by-case basis. For example: in Year 2, we might want a chat-style "ask about your cost" assistant. That's a new feature where the LLM is more central, but it would be a separate subsystem, not the methodology.

## Open questions

- Whether to support customer-provided LLM keys (BYO Anthropic key) for customers who don't want their workload metadata going through Optiqor's Anthropic account. Lean toward yes for enterprise tier; defer the implementation.
- Whether to ship a self-hosted small model for air-gapped customers. Year 2-3 question; relevant for Optiqor Compliance.
- Prompt caching strategy (specific to Anthropic's prompt caching feature). Operational decision, not architectural.

## Implementation status

**Partial.** LLM orchestration lives at `internal/agent/llm/` (target rename to `internal/explain/`); `FakeLLMClient`, sanitizer (`internal/agent/llm/sanitizer`), and budget cap (`internal/agent/budget`) all shipped and tested. Real Anthropic SDK wiring is a one-file add behind the interface, deferred until API key + caching dashboards land. CI lint rules enforcing the isolation invariants (methodology can't import LLM, explain returns strings only) are not yet active; they land with the first methodology PR.

*Last verified: 2026-05-18.*
