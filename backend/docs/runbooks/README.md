# Optiqor on-call runbooks

What to do when a page fires. Every alert in `rules/optiqor-slo.yaml`
points at a runbook here. Add a new alert → add a new runbook in the
same PR; an unrouted alert is the failure mode this directory exists
to prevent.

Conventions:

- One file per alert / failure mode.
- Title is the alert name (e.g. `agent-snapshot-rls-isolation-failure.md`).
- Body answers four questions, in order:
  1. What this means.
  2. How to verify the bad state.
  3. What to do right now to stop the bleeding.
  4. What to do later to keep it from happening again.
- Customer-facing links (status pages, support emails) come last so the
  responder copies them into a thread without digging.

When the page is ambiguous, write a runbook before the next on-call
shift. The cost of a 10-minute write is one prevented late-night Slack
thread.
