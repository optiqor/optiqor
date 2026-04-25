# Costify Open-Source CLI Playbook

> **The viral acquisition layer. Ship the OSS CLI, win the category.**

> ## Amendments — 2026-04-26
>
> The CLI's Year 1 plan is largely intact (Apache 2.0, npm distribution, no LLM, no telemetry, ±40% accuracy disclosure mandatory). Three additions reflect the broader product expansion:
>
> - **CLI works against any conformant K8s from Day 1.** The CLI was already cluster-agnostic at the Helm-parsing layer; `costify analyze --cluster <kubeconfig>` now also works against AKS, Hetzner, on-prem from launch. Output renders Cloud Receipt or Capacity Receipt depending on what the cluster's billing context supports.
> - **GitLab MR upload via `--share` accelerated.** Originally Year 2; now Phase 8 (Months 6–9) when GitLab GA lands in the SaaS. The CLI's `--share` upload endpoint accepts both GitHub and GitLab artifact URLs.
> - **Detector SDK seed in Year 1, not Year 2.** Original plan deferred `costify/detector-sdk` to Year 2. Recommendation: ship a *seed* version in Phase 9 (Months 9–12) with the 30 detectors already in production extracted into the SDK shape, even if the public release is rough. This builds community contribution muscle a year earlier and creates a moat that's harder for late entrants to replicate.

Written for whoever owns open-source + DevRel. This is the execution spec for the `github.com/Costify/cli` repository and its satellite integrations. Follows the Infracost playbook but adapted for Kubernetes.

---

## 1. Strategic Role

The Costify OSS CLI is **not the product**. It is the **acquisition funnel to the product**.

- The SaaS platform (Apply Fix, Receipts, Auto-Rollback, Cost Spike) requires cluster install and cannot run from Helm files alone
- The CLI delivers directional value from Helm files alone — enough to prove the product is real
- Every CLI output funnels the user toward installing the agent for exact numbers

**Target metrics:**
- Month 6: 1,500 combined stars across OSS repos
- Month 12: 5,000 combined stars
- Month 18: 12,000 stars (Infracost parity)
- Month 24: 20,000+ stars
- CLI-to-SaaS conversion: 8%+ of CLI users sign up for SaaS within 90 days

---

## 2. What We Ship (and What We Don't)

### Ships Open Source (Apache 2.0)

| Repository | Purpose | Ship By |
|-----------|---------|---------|
| `Costify/cli` | The viral CLI (`npx @Costify/cost`) | Week 6 |
| `Costify/actions` | GitHub Action wrapping the CLI | Week 7 |
| `Costify/agent` | In-cluster agent (enterprise unlock) | Week 10 |
| `Costify/detector-sdk` | SDK for writing custom detectors | Year 2 |
| `Costify/helm-bench` | Public Helm chart efficiency database | Month 4 |

### Explicitly NOT Shipping (Year 1-2)

- VS Code / JetBrains / Neovim extensions — our user lives in GitHub PRs, not IDE
- GitLab CI / Bitbucket integrations — defer to Year 2 when we support non-GitHub VCS
- Anything that competes with our own paid SaaS tier

**Discipline rule:** if opening it doesn't earn us GitHub stars or enterprise unlock, we don't open it.

---

## 3. The 12 Launch Features

Ranked by viral-per-engineering-hour. First 5 ship for Month-3 HN launch.

### Tier 1: Launch Set (Must ship before HN post)

**Feature 1 — `Costify analyze <chart>` (the core command)**

The single most-important command. Outputs the money-shot ASCII report:

```
$ Costify analyze ./checkout-chart

╔═══════════════════════════════════════════════════════════╗
║              Costify Cost Analysis                      ║
║                                                           ║
║   Chart:       ./checkout-chart                           ║
║   Services:    7 (6 Deployments, 1 StatefulSet)           ║
║                                                           ║
║   💰 Reserved capacity cost:    $2,560/month (upper bound)║
║   📉 Estimated potential:       $940/month saved (37%)    ║
║                                                           ║
║   ⚠️  Top offenders:                                       ║
║      checkout-api       over-provisioned CPU    -$420/mo  ║
║      inventory-svc      over-provisioned mem    -$280/mo  ║
║      payment-worker     3 idle replicas         -$240/mo  ║
║                                                           ║
║   💡 Fix preview (apply with cluster install):            ║
║      checkout-api.resources.requests.cpu: 1000m → 400m    ║
║      inventory-svc.resources.requests.mem: 4Gi → 1.5Gi    ║
║      payment-worker.replicas: 5 → 2                       ║
║                                                           ║
║   Grade: D+                                               ║
║   Better than 23% of similar Helm configs                 ║
║                                                           ║
║   ⚡ Accuracy: ±40% (sandbox mode, Helm only)             ║
║      Install agent for exact numbers → Costify.dev/get  ║
║                                                           ║
║   🔗 Share this report:                                   ║
║      https://Costify.dev/r/a3f9b2                       ║
╚═══════════════════════════════════════════════════════════╝
```

**Critical design decisions:**
- **Honest accuracy disclosure** (±40%) — builds trust rather than breaking it when numbers don't match CUR later
- **Grade (D+) + percentile (23%)** — status comparison, shareable
- **Named offenders** — "checkout-api" is shareable; generic "CPU issue" isn't
- **Shareable URL** — generated by default, 30-day expiry, no login required
- **CTA to install** — converts CLI users to SaaS users

**Feature 2 — `Costify demo` (no-setup trial)**

Removes the biggest friction point: "I don't have a chart handy."

```
$ npx @Costify/cost demo

Pick a sample to analyze:
  1. Typical e-commerce stack      (12 services, mixed efficiency)
  2. bitnami/postgresql            (known over-provisioned defaults)
  3. ingress-nginx                 (well-tuned reference)
  4. Deliberately-bad chart        (educational — find 20 issues)
  5. Your own chart (provide path)

Choose [1-5]:
```

**Why it matters:** First-time user experience without any barrier. Option 4 doubles as a training/demo tool Costify SEs can use in sales calls.

**Feature 3 — `Costify diff <base> <head>` (the killer feature)**

PR-time before/after comparison:

```
$ Costify diff main feature/scale-for-black-friday

📊 Cost Impact:  +$340/month  ↑
📊 Security:     +2 findings (1 high, 1 medium)

Changes in this PR:
   checkout-api
   ├─ replicas       3 → 8          +$280/mo
   ├─ cpu.requests   500m → 1500m   +$45/mo
   └─ privileged     false → true   ⚠️ HIGH SECURITY

   inventory-svc
   └─ image          stable → :latest  ⚠️ MED: unpinned tag

Verdict: ⚠️ MERGE WITH CAUTION
  This PR increases costs and introduces security findings.

🔗 Full report: https://Costify.dev/r/abc123

Want this as a PR comment automatically?
→ Add Costify/actions@v1 to your workflow
```

**Why it matters:** this is the feature Infracost built their entire company on (the PR diff comment). Ours ships the same day-one and adds security-per-PR alongside cost.

**Feature 4 — Shareable report URL (viral loop)**

Every analysis auto-generates `Costify.dev/r/<hash>`. The public report page is designed for screenshots:

- Large "$78K/year potential savings" headline above the fold
- Social-card optimized (LinkedIn, Twitter, Slack unfurl beautifully)
- Prominent "Analyze your own" CTA
- 30-day URL expiry (builds optional account funnel)
- Optional `--offline` flag for users who can't share (regulated environments)
- Optional `--private` flag for login-gated links

**Why it matters:** every shared link is free distribution. Every LinkedIn post about "look what Costify found" = inbound demos. This is the viral loop.

**Feature 5 — `Costify score` (gamification)**

```
$ Costify score ./my-chart

🧠 Costify Score: 62/100        Grade: C+

Breakdown:
  Cost Efficiency       55/100   ↓ below median
  Security Hygiene      78/100   ↑ above median
  Reliability Risk      60/100   = median
  Resource Utilization  55/100   ↓ below median

📊 Industry comparison (anonymized, 2,847 Helm charts):
   Your tier:          C+  (bottom 40%)
   Median:             B−  (score 72)
   Top 10%:            A   (score 89+)

🏆 Top 3 improvements for biggest score jump:
   1. Add HPA to 4 services       → +8 points
   2. Pin container image tags    → +5 points
   3. Set memory requests         → +4 points

Track your score over time: Costify.dev/score/<cluster-id>
```

**Why viral:** comparative scoring (percentile tier) triggers social sharing. CTOs screenshot this. Platform teams compete internally. Becomes an industry benchmark over time.

### Tier 2: Post-Launch (ship Month 4-5)

**Feature 6 — GitHub Action wrapping `diff`**

```yaml
# .github/workflows/Costify.yml
- uses: Costify/actions@v1
  with:
    chart: ./helm
    # Optional: link to installed Costify SaaS for exact numbers
    saas-token: ${{ secrets.Costify_TOKEN }}
```

Posts the diff as a PR comment automatically on every push. Distribution multiplier — every repo using the action is free brand exposure on every PR.

**Feature 7 — `Costify compare <chart1> <chart2>`**

Side-by-side comparison of two Helm charts. Press-bait feature:

```
$ Costify compare bitnami/postgresql cloudnative-pg

Winner: cloudnative-pg (48% cheaper, better defaults)

                    bitnami/postgresql    cloudnative-pg
Monthly cost        $280                  $145  (-48%)
Grade               C+                    A−
Replicas default    2                     1
HPA included        No                    Yes
NetworkPolicy       No                    Yes
```

Chart maintainers will argue in the HN comments. Press will cover the results. SEO for "bitnami vs cloudnative-pg" etc.

**Feature 8 — `Costify watch` (live terminal dashboard)**

```
┌─────────────────────────────────────────────────────┐
│ Costify Live │ checkout-cluster │ 14:32 updated   │
├─────────────────────────────────────────────────────┤
│                                                     │
│ 💰 Cost today:       $420  ↑ +12% vs yesterday      │
│ 📊 Grade:            B−  (stable)                   │
│ ⚠️  New findings:    2 in last hour                 │
│                                                     │
│ Live activity:                                      │
│   14:31  PR #4821 opened — +$340/mo predicted       │
│   14:28  checkout-api OOMKilled (3rd in 24h)        │
│   13:55  Costify suggested fix merged — saving…   │
│                                                     │
│ [q]uit  [r]efresh  [s]hare                          │
└─────────────────────────────────────────────────────┘
```

Requires cluster install (reads from Costify SaaS). Habit-forming dashboard that lives in a tmux pane. GIF-recordable for Twitter.

### Tier 3: Distribution Multipliers

**Feature 9 — Public Helm Chart Efficiency Leaderboard**

Published at `Costify.dev/charts`:

```
🏆 Most Efficient Public Helm Charts (Q2 2026)

1. cloudnative-pg         Grade: A+  (26K installs tracked)
2. cert-manager           Grade: A
3. external-dns           Grade: A−
...
47. bitnami/postgresql    Grade: C   ← over-provisioned defaults
...
88. bitnami/mongodb       Grade: D   ← way over-provisioned
```

**Why viral:** every chart name is an SEO term. Chart maintainers will care about their grade. Press will cover "Costify report: 63% of popular Helm charts ship over-provisioned."

This becomes the Costify moat — anonymized aggregated data that no competitor can replicate.

**Feature 10 — README Badge (shields.io-style)**

```markdown
[![Costify Grade: A−](https://Costify.dev/badge/bitnami/postgresql)](https://Costify.dev/charts/bitnami/postgresql)
```

Chart maintainers add it for signaling. Consumers add it for due-diligence signaling. Every badge is a backlink and brand impression.

**Feature 11 — `Costify audit` (live cluster read-only scan)**

Requires cluster install. Full cluster health report — the demo feature for sales calls:

```
$ Costify audit --kubeconfig ~/.kube/config

Scanning cluster... (this will NOT modify anything)

📊 Cluster Health Report
────────────────────────
Nodes:       23
Workloads:   147 deployments, 12 statefulsets
Utilization: 18% avg (target: 60%+)
Monthly bill: ~$47,200

Top opportunities:
  1. Over-provisioned CPU     → potential $8,400/mo
  2. Unused PVCs              → potential $1,200/mo
  3. Missing HPAs             → potential $2,800/mo

Security:
  ✗ 3 services with privileged: true
  ✗ 12 services without resource limits
  ✗ 4 services using :latest tag

Full report → Costify.dev/r/xyz
Upload to Slack with: Costify share
```

**Feature 12 — `Costify --roast` mode**

Optional humor mode. Opt-in via flag. HN-launch press-bait:

```
$ Costify analyze --roast ./my-chart

😤 Alright, let's see what we have here...

• 8 CPUs requested, using 0.3?
  "I'm saving them for a rainy day" — your PR, apparently

• 32Gi memory limit, peak usage 2.1Gi?
  This isn't a database, it's a hoarder simulator

• 5 replicas of a service processing 4 req/min?
  Sir, this is a Wendy's

Total damage: $2,840/month

You could buy a MacBook Pro every 2 weeks with this money.
Instead you're feeding spot instances that aren't even hungry.

Fix it → Costify.dev/r/abc
```

Reddit-front-page energy. Trigger with `--roast`; default is `--serious` for enterprise.

---

## 4. Output Design Rules

Lessons stolen from Kerno (eBPF incident diagnosis tool with an excellent README) and Infracost. Apply to all CLI output:

### Rule 1: Boxed findings pattern

Each critical finding gets a boxed header:

```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ 💰  HIGH WASTE  ·  CPU Over-Provisioned              -$420/mo   ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
   Service    checkout/checkout-api
   Current    CPU requests   ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇  1000m
              P95 actual     ▇▇▇▇▇▇                280m
   Cause      Reserved 3.6× typical usage
   Impact     $420/month in unused capacity
   Fix        → cpu.requests: 1000m → 400m (40% headroom over P95)
```

Pattern: **boxed header → identifier → signal with visual bars → cause → impact → fix.**

### Rule 2: Always label accuracy tier

Every cost number carries a confidence label:

- `±40%` = sandbox/CLI (Helm only)
- `±15%` = cluster-connected (Prometheus data)
- `±5%` = receipt-grade (CUR verified)

Users know exactly what they're looking at. No surprises when the bill arrives.

### Rule 3: Every CLI output ends with a CTA

Three standard CTAs:

1. **Share CTA:** `🔗 Share this report: Costify.dev/r/<hash>`
2. **Upgrade CTA:** `👉 Install agent for exact numbers: Costify.dev/get`
3. **Learn more CTA:** `📚 How Costify works: Costify.dev/how`

Always one. Never three at once — pick the one most relevant to the output.

### Rule 4: Deterministic rules first, LLMs second

(Borrowed directly from Kerno's engineering philosophy.)

- The rule engine is pure Go, testable, runs without LLM
- Every finding has a clear cause, threshold, and fix
- LLMs are post-processors — they write the Apply Fix diff and explain reasoning
- LLMs never decide *what* is wrong; only *how* to fix what the rules flagged
- CLI runs entirely deterministically for the sandbox case (no LLM calls, no customer data leaves the machine)

---

## 5. The Launch Plan

### Month 0–1 — Foundation
- `Costify/cli` repo created, Apache 2.0
- `Costify analyze` + `Costify demo` shipping
- npm package published: `@Costify/cost`
- Landing page at `Costify.dev/cli` with installation docs
- Private beta with 10 friendly platform engineers

### Month 2 — Expansion
- `Costify diff` shipped
- GitHub Action wrapping it
- Shareable URL system live
- `Costify score` shipped
- README polished with boxed finding examples
- 20 friendly early users generating feedback + stars

### Month 3 — "Show HN" Launch

**Title:** "Show HN: We analyzed 2,847 public Helm charts. 73% are over-provisioned."

**Post structure:**
1. Opening: the finding (63% of popular charts ship over-provisioned)
2. Screenshot of `--roast` output on `bitnami/postgresql`
3. Link to public leaderboard at `Costify.dev/charts`
4. Installation: `npx @Costify/cost demo`
5. Methodology link (transparent — we're honest about ±40% accuracy)
6. Invitation: "Run it on your chart, reply with your grade"
7. 2 engineers on standby to answer every HN comment for 48 hours

**Preparation checklist:**
- Public leaderboard live with 2,500+ analyzed charts
- `Costify/cli` repo with excellent README (steal Kerno's structural template)
- 5 real customer Receipts to cite (from design partners)
- KubeWeekly and CNCF Slack outreach scheduled for same day
- Twitter/LinkedIn announcements from founders pre-written

**Expected outcome (benchmarked against Infracost's launch):**
- Top 10 HN for 12+ hours
- 1,500-3,000 CLI installs in 72 hours
- 500-1,000 GitHub stars in week 1
- 2-3 press pickups (TechCrunch, The Register, KubeWeekly)
- 10+ inbound VC messages
- 20-30 design partner inbound requests

### Month 4–6 — Distribution
- `Costify compare` shipped (press-bait for chart comparisons)
- `Costify/actions` on GitHub Marketplace
- README badge adoption drive (email 100 popular chart maintainers)
- `Costify/helm-bench` dataset public
- KubeCon EU booth + conference talk
- First 100 badge adoptions on popular charts

### Month 6–12 — Compounding
- `Costify watch` (live dashboard, drives daily habit)
- `Costify audit` (live cluster scan, sales tool)
- `--roast` mode (HN launch #2 — "We roasted 100 bad Helm configs")
- Year-end "State of Kubernetes Efficiency" report with anonymized data
- CNCF Silver membership + KubeWeekly sponsorship
- 12K star target hit

---

## 6. What We Can't Learn from Infracost's Playbook

Three things we're doing differently:

**1. Security-per-PR from Day 1.** Infracost is cost-only. We ship security findings in the same PR comment. This is a genuine differentiator — cost is the viral wedge, security is the stickiness.

**2. Shareable URLs by default.** Infracost's output is great but stays in your terminal. Our `Costify.dev/r/<hash>` URLs are designed for Slack-sharing and LinkedIn posts. Viral loop that Infracost missed.

**3. Public Helm chart leaderboard.** Infracost never published a comparative ranking of Terraform modules. We'll publish one for Helm charts on Day 1. Chart maintainers care, press covers it, SEO compounds.

---

## 7. What We DON'T Build

Explicitly on the "not doing" list, with business reasons:

| Thing | Why Not |
|-------|---------|
| VS Code / JetBrains extensions | Our user lives in GitHub PRs, not IDE. Helm is GitOps-native. Low signal. |
| GitLab CI / Bitbucket integrations | Wait until we support non-GitHub VCS (Year 2) |
| Terraform support in CLI | Not our wedge. Infracost owns it. Year 2 decision. |
| Full-featured TUI beyond `watch` | Feature creep. Focus on CLI + web. |
| Electron desktop app | Comical. No. |
| Windows-native binaries | Maintenance burden; `npx` works everywhere via Node. |
| SDKs for every language | Ship Go SDK first (it's what the CLI is). Add TypeScript only when community asks. |

---

## 8. Success Metrics

| Month | Stars | Weekly Installs | Shareable URLs Generated | CLI→SaaS conversion |
|-------|-------|-----------------|--------------------------|---------------------|
| 1 | 100 | 200 | 500 | - |
| 3 | 1,500 | 5,000 (HN bump) | 12,000 | 3% |
| 6 | 3,500 | 3,000 (steady) | 30,000 | 5% |
| 12 | 8,000 | 5,000 | 60,000 | 7% |
| 18 | 12,000 | 8,000 | 100,000 | 8% |
| 24 | 20,000+ | 12,000 | 150,000+ | 9% |

**Leading indicators we watch weekly:**
- Star velocity (stars/week)
- `npx Costify` install rate
- Shareable URL generation rate
- CLI → SaaS sign-up funnel conversion
- GitHub Action adoption (`.github/workflows/Costify.yml` files)

**Lagging indicators we watch monthly:**
- CLI users → SaaS paid customers conversion
- Revenue attributable to CLI-discovered customers
- Press mentions per month
- KubeCon talk acceptance rate

---

## 9. Summary

The Costify CLI is the viral acquisition layer for the SaaS product. It delivers directional value from Helm files alone (sandbox-grade accuracy, ±40%), honestly communicates its own limitations, and funnels every user toward agent install for the full product. Twelve core features, sequenced across 12 months, anchored by a Month-3 Show HN launch that follows the Infracost playbook. Target: 12,000 GitHub stars by Month 18. Everything else — community growth, VC inbound, enterprise pilots — flows from those stars and the `Costify.dev/r/<hash>` URLs that get shared daily in platform-engineer Slack channels.

We don't win because our CLI is better than Infracost's. We win because we ship the same playbook one year later in the adjacent Kubernetes category and layer security, Receipts, and Auto-Rollback on top.

---

*Document version: 1.0 · Standalone OSS CLI playbook · April 2026*
