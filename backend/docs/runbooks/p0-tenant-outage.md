# P0: tenant outage

A tenant reports the dashboard is dead or no PRs are landing. This
runbook is for the first 30 minutes of triage. Long-form post-mortem
is a separate doc.

## First five minutes

1. Open `/app` as the tenant via Auth.js impersonation. Goal: tell
   real outage apart from "their cluster's RBAC drifted and the agent
   stopped reporting."
2. Check the Agent Health pill:
   - Green → SaaS side problem; skip to step 3.
   - Amber / red → cluster-side problem; cut to
     [agent-install.md](agent-install.md) and confirm RBAC / Prom
     reachability before paging dev.
3. Hit `/v1/onboarding/health` for the tenant; the Blockers list
   surfaces the canonical next step they're stuck on. If the response
   500s, the api binary itself is broken — move to step 4.
4. Check `/metrics` on the api binary; look for `optiqor_http_5xx_total`
   climbing. Cross-reference with Grafana
   ([api-latency dashboard](https://grafana.internal/d/api-latency)).
5. If the api is down: spin a fresh replica via the Helm rollback
   path; the in-cluster Temporal queue absorbs the gap.

## What never to do

- Don't `kubectl exec` into the api pod and edit anything. The pod is
  immutable.
- Don't bypass RLS to "just check the data." Every cross-tenant query
  goes through `set_superuser_context()` with a recorded reason; if
  you can't justify the reason, you can't justify the query.
- Don't merge any backend change while the outage is live. The "fix
  for the outage" almost always belongs in a feature flag, not a
  branch merge.

## Customer comms

Default to silence until you know what's wrong. A "we're looking at
it" message before the diagnosis is in is worse than 15 minutes of
silence because the customer reads it as "you're confused." Once the
diagnosis is in, ship one message: what happened, what the impact
window is, what fixed it, and a non-promise to follow up with a
post-mortem in 5 business days.

## After

- File the incident in Linear with `incident-` prefix.
- Add a recording rule + alert if this could have been caught
  pre-page.
- Add a new runbook file in this directory linked from the alert.
- Schedule the post-mortem inside 5 business days.
