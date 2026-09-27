## What

<!-- One-paragraph summary of the change. -->

## Why

<!-- Link to ticket / ADR / customer issue. Why now? -->

## How

<!-- Implementation notes. Highlight anything reviewers should look at carefully. -->

## Risk & rollout

- [ ] Migration included (if yes, describe rollback path)
- [ ] Behind a feature flag (if user-visible behavior change)
- [ ] Touches tenant-scoped data path (if yes, RLS verified end-to-end)
- [ ] Touches secrets / credential handling (if yes, security review requested)

## Verification

<!-- How was this tested? Unit / integration / e2e / manual against staging. -->

## Checklist

- [ ] `make ci` passes locally
- [ ] No new TODOs without an issue link
- [ ] No new dependencies without a justification in the PR description
