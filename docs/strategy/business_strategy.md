# Sevro — Business Strategy (Kubernetes-First)

> **A defensible path from Kubernetes beachhead to $500M+ ARR cost-and-security PR platform.**

> ## Amendments — 2026-04-26
>
> Year 1 wedge expanded from "AWS EKS + GitHub" to a multi-cloud / multi-VCS surface that actually defends "we own Kubernetes" instead of "we own EKS." Original tables and forecasts retained below for historical context.
>
> **Beachhead (revised):** AWS EKS + Azure AKS + Hetzner Cloud K8s, with GitHub + GitLab and ArgoCD + Flux CD. Estimated Year-1 addressable target: ~9,500 K8s-GitOps orgs (was ~6,000 EKS-on-GitHub).
>
> **Three-tier Receipt model** (Cloud / Capacity / Hybrid) — see [idea.md amendments](idea.md). The Capacity Receipt is the technical bet that lets us claim "every K8s, anywhere" instead of "AWS-only."
>
> **Pricing model unchanged.** Still keyed off monitored K8s monthly spend; Hetzner customers price against EUR-equivalent; Capacity Receipts price against deferred hardware purchase value.
>
> **Funding path (revised):**
>
> | Round | Target | Was | Reason |
> | --- | --- | --- | --- |
> | Seed | **$12–15M** | $8–12M | Fund expanded Year 1 (AKS + Hetzner + GitLab + Flux) |
> | Series A | $20–30M | $15–25M | Funded scope is broader; multi-cloud GA already proven |
> | Series B | $50M+ | $40M+ | EU + regulated motion accelerated by 6 months |
>
> **Year 1 ARR targets unchanged** ($4.8M base / $3M floor) — broader surface offsets slower per-segment ramp. Year 2 targets *raised* slightly because GitLab + Hetzner unlock the EU mid-market 12 months earlier than the original plan.
>
> **Hiring revision:** Engineers #4 and #5 by Month 4 (was Month 7). DevRel by Month 6 (unchanged). Solutions Engineer #1 by Month 9 (added — needed for AKS + Hetzner enterprise pilots).
>
> **Items pulled out of Year 2 backlog into Year 1:** AKS support, Hetzner Cloud, GitLab, Flux CD.
> **Items remaining in Year 2:** GKE, Bitbucket, Terraform-K8s-adjacent, Detector SDK (full release), on-prem / air-gapped, customer-hosted (in-VPC) deployment, GraphQL API (defer until customers ask), Go SDK.
>
> **Six-metric health framework added** — Activation Rate (≥60% Y1), Time to First Receipt (≤35 days p50), Gross Retention (≥95%), Net Revenue Retention (≥120% Y1, ≥130% Y3), per-tenant Health Score (0–100, alert <50), Leading Churn Indicator (<5% of base). 130% NRR matches Snowflake/Datadog/MongoDB at IPO. Full implementation Phase 9; activation tracking lights up Phase 5.
>
> **Per-tenant cost-to-serve math (Y1 targets):**
>
> | Tier | Profile | Cost-to-serve | Revenue | Gross margin |
> | --- | --- | --- | --- | --- |
> | Team | $500/mo, 1 cluster, ~20 workloads, ~10 PRs/day | $65–110/mo | $500/mo | **78–87%** |
> | Enterprise | $100K/yr, 10 clusters, ~200 workloads, ~80 PRs/day | $520–865/mo | $8,333/mo | **89–94%** |
>
> Drives the gross margin walk: ≥65% Month 12, ≥78% Month 18, ≥75% by Y3 (was ≥65% Y2 / ≥75% Y3 — accelerated by 12 months because LLM cost discipline + cache hit rate are now Year 1 hard targets, not soft).
>
> **Onboarding time-to-value SLOs are committed in-product:** Sandbox p95 < 3s · App install → first PR comment < 10 min · Agent install → first recommendation < 30 min · Agent install → first Cloud Receipt < 35 days. These are not aspirational — missing any of them is a P1 incident.
>
> **GDPR + EU data residency accelerated to Year 1.** Original plan deferred `eu-west-1` to Year 2; pulling GitLab + Hetzner into Y1 means EU customers from Phase 8 (Month 9) — incompatible with US-only deployment.
>
> - **Phase 1 baseline:** DPA template (DocuSign), public subprocessor list with RSS feed, DSAR endpoints (`export` + `erase` with 30-day purge + tombstones), enforced data retention (Prometheus 90d / LLM logs 30d / Receipts 7y / audit log 7y), PII minimization in LLM prompts.
> - **Phase 8 EU GA:** full `eu-west-1` deployment, region selection at signup (cannot be changed post-signup), Anthropic EU-residency endpoint exclusively for EU tenants, EU-resident KMS Receipt-signing key, transparency log shards per region.
>
> Stripe Billing infrastructure scoped into Phase 6 (was missing from original plan): subscriptions + customer portal + usage metering + plan limits + 14-day trial + annual billing + Stripe Tax for EU VAT.
>
> Disaster Recovery targets committed for SOC 2 Type 1 audit at Month 9: RDS PITR (RPO 5min / RTO 30min) · Receipt-signing keys multi-region replicated (RPO 0 / RTO 5min) · Transparency log replicated to S3 + offsite (RPO 0) · S3 Cross-Region Replication on Receipt + sandbox buckets · monthly automated restore drill · quarterly manual fire drill with rotating drill leader.

---

## 1. Executive Summary

Sevro is not a remediation product. It's the **Kubernetes Control Plane for Cost and Safety** — the gatekeeper between commit and cluster that every K8s change flows through. Kubecost and OpenCost live in the cluster dashboard. Cast AI autopilots clusters as a black box. Infracost parses Terraform, not Kubernetes. Kubecost attempted the PR-layer category in 2023 with a cost-prediction GitHub Action — the repo has 31 stars and hasn't been touched in three years, proving the category is hard, not occupied. We're building the platform that defines it.

- **Positioning:** "Kubecost showed you the price. Sevro is the gate every K8s change flows through — cost, security, compliance, receipts, rollback."
- **Beachhead:** AWS EKS + GitHub + Helm/Kustomize + ArgoCD — ~6,000 target companies globally in Year 1, expanding to ~30,000 K8s-GitOps orgs by Year 3
- **Trust contract:** Auto-Rollback Guarantee + verified Receipts + honest Confidence bands + mission-preserving governance — the first working K8s remediation platform that earns enterprise autopilot
- **Platform play:** API, Detector SDK, Apply Fix Marketplace. Integration surface for Wiz, Datadog, PagerDuty, Kubecost, cloud marketplaces
- **Open-source discipline:** One public repo (`github.com/sevro/agent`, Apache 2.0). Everything else commercial. The agent earns us the enterprise market (source-auditable requirement); the platform stays closed to protect the moat. Target ratio: ~10% open, 90% commercial — closer to Datadog and Stripe than HashiCorp.
- **Moat:** Cross-customer anonymized data (unfakeable), receipt reputation (slow to earn), CNCF community presence (compounds), 5+ year integration lock-in with every major K8s tool
- **Market:** ~$180M K8s-PR SAM today, ~$1.2B full-cloud SAM by Year 3 as we expand outward; TAM is $3B+ at Year 5 as category expands
- **Pricing:** Based on monitored K8s monthly spend, not developers. Team from $500/mo, Team+ from $3,500/mo, Enterprise $100K/yr base + savings-share
- **GTM:** Six-channel parallel distribution (sandbox, CLI, HN, Marketplace, SEO, Twitter/LinkedIn, KubeCon/CNCF). Design partners → free tier → paid Team → Enterprise
- **Funding path:** $1.5-2M pre-seed → $8-12M seed → $30-50M Series A → $100-150M Series B (Month 30-36) → $200-300M Series C (Year 4) → $5-10B IPO Year 6
- **Domination trajectory (base case for this plan):** $4.8M ARR Year 1, $47M Year 2, $231M Year 3, $616M Year 4, $1.3B Year 5 · 60% penetration of K8s-GitOps orgs by Year 5
- **Floor case (if domination fails):** $3M ARR Year 1, $22M Year 2, $95M Year 3, $260M Year 4, $527M Year 5 · still a billion-dollar outcome

**The thesis is not "build a good K8s remediation product and sell to Datadog."** The thesis is: define the Kubernetes cost-and-safety category, earn CNCF community gravity, build an unfakeable data moat through cross-customer receipts, and IPO as the Datadog of K8s FinOps + Safety — $500M+ ARR, independent, category-defining.

---

## 2. Market Opportunity

### 2.1 Why Kubernetes-First Is the Right Wedge

Three facts make K8s the right beachhead:

**1. K8s cost is where the money is.** For mid-market tech companies, Kubernetes typically represents 30–60% of the cloud bill — and the wasteful 30–60%. This is a bigger pool than pure Terraform cost optimization.

**2. K8s changes are already PR-native.** Every Helm values edit, every Kustomize overlay, every ArgoCD Application goes through a pull request. The workflow hole is pre-dug; we fill it.

**3. The PR remediation layer is empty.** Kubecost monitors clusters. Cast AI / ScaleOps / Sedai autopilot clusters. Infracost parses Terraform. OpenCost exports metrics. Kubecost tried the PR category in 2023 with a cost-prediction Action and abandoned it at 31 stars (still v0.1.1). No one has shipped a working Kubernetes PR remediation product — not because the problem isn't real, but because the hard version (Apply Fix + Prometheus-grounded confidence + verified Receipts + Auto-Rollback) is harder than the easy version (a static cost comment).

### 2.2 Market Sizing (Bottom-Up, Honest)

**Year 1 SAM — Kubernetes PR remediation:**

| Filter | Count | Source |
|--------|-------|--------|
| Global orgs with production Kubernetes in 2026 | ~90,000 | CNCF Annual Survey |
| With meaningful K8s spend (>$30K/month) | ~20,000 | CNCF + Flexera estimates |
| Using Helm or Kustomize in production | ~75% = 15,000 | CNCF tooling survey |
| Using GitOps with ArgoCD or Flux | ~60% = 9,000 | ArgoCD + Flux adoption data |
| On GitHub (vs GitLab/Bitbucket) | ~70% = **6,300** | GitHub Octoverse + Stack Overflow |

**Year 1 Serviceable Addressable Market: ~6,300 companies × $28K blended ACV = ~$180M.**

Not a billion-dollar SAM in Year 1, and we're honest about it. But it's a focused beachhead where we can achieve 10–20% penetration within 3 years.

### 2.3 How the Market Expands

| Phase | Expansion | Adds to SAM |
|-------|-----------|-------------|
| Year 2 | Add GKE + AKS (multi-cloud K8s) | ~4,000 companies (+$110M) |
| Year 2 | Add GitLab + Bitbucket | ~2,500 companies (+$70M) |
| Year 2 | Add Terraform for K8s-adjacent cloud resources | ~10,000 companies (+$300M) |
| Year 3 | Add Flux CD support | ~2,000 companies (+$56M) |
| Year 3 | Add full non-K8s cloud cost (head-to-head with Infracost) | ~8,000 companies (+$240M) |
| Year 3 | Add AI/LLM infrastructure cost analysis | ~2,000 companies (+$60M) |
| **Year 3 expanded SAM** | | **~$1.2B** |

Adjacency expansions (security-per-PR at Wiz parity, carbon-per-PR, SaaS cost detectors) bring total TAM to **~$3–5B by Year 5** — a real venture-scale opportunity grounded in defensible numbers.

### 2.4 Why Now

1. **KubeCon energy is peaking.** Platform engineering is now a distinct discipline with dedicated teams, conferences, and budget. This buyer persona didn't exist at scale 3 years ago.
2. **Cast AI's autopilot is controversial.** Platform teams love the savings but hate the black box. An auditable PR-based alternative is in demand.
3. **LLMs reached the reliability bar for Helm generation.** Claude Sonnet 4+ can produce validated Helm values diffs with 90%+ first-try correctness on common patterns.
4. **The CNCF FinOps Foundation partnership is elevating K8s cost as a board-level topic.** EU CSRD regulations coming 2026–2027 add compliance urgency.

The window is open. 18–24 months to claim the category before Infracost or Cast AI pivot into the PR layer.

---

## 3. Ideal Customer Profile

### 3.1 Primary ICP (Year 1)

- **Stage:** Series B to Series D tech companies
- **Headcount:** 80–500 engineers
- **Kubernetes footprint:** 3–25 production clusters on AWS EKS
- **Kubernetes spend:** $30K–$400K/month (30–60% of total cloud bill)
- **GitOps stack:** ArgoCD + Helm + Prometheus
- **Platform team:** 3–15 engineers with a dedicated Head of Platform
- **Pain:** Platform team knows the cluster is overprovisioned; product engineers won't rightsize; Kubecost dashboard ignored; Cast AI rejected by security review

### 3.2 Secondary ICP (Year 2)

- Mid-market SaaS ($100M–$1B revenue) with multi-cloud K8s (GKE + EKS)
- Compliance-driven orgs with SOC 2 / HIPAA / PCI requirements
- Platform teams with formal FinOps partnership

### 3.3 Enterprise ICP (Year 3+)

- Fortune 1000 with significant K8s footprint across 50+ clusters
- $10M+ annual cloud spend, of which >40% is K8s
- Procurement-led sales cycles
- Deals $250K–$1M+ ACV

### 3.4 Who We Do NOT Serve (Year 1)

- Orgs without Kubernetes
- Orgs on GitLab / Bitbucket / Azure DevOps (until Year 2)
- Orgs without Prometheus or metrics-server deployed
- Air-gapped / on-prem-only clusters
- Teams that manage infrastructure through ClickOps or kubectl-apply patterns (until we ship Shadow Mode in Year 3)

---

## 4. Value Proposition

### 4.1 For Platform Engineers (Champion)
- Install in 2 clicks via Helm chart + GitHub App
- See Apply Fix PRs on every overprovisioned service within 24 hours
- Confidence Score on every change grounded in 30 days of Prometheus data
- Never need to log into another dashboard — Sevro lives in the PR
- Earn public recognition via Engineer Impact score + receipts

### 4.2 For Heads of Platform (Buyer)
- K8s bill trending down measurably each quarter
- Auditable trail of every cost-related change across clusters
- Sevro Score benchmarks the team against peer companies
- Auto-Rollback Guarantee makes delegation to the AI safe

### 4.3 For CFOs and FinOps
- Answer "what caused the K8s spike?" in seconds instead of weeks
- Verified savings receipts for every optimization, signed cryptographically
- Clear ROI via pay-for-savings pricing
- Predictable K8s bills, no more month-end surprises

### 4.4 For CISOs
- Security findings on K8s manifests remediated at merge time
- Apply Fix writes the secure configuration — no Jira handoff
- Zero production write access required
- Full audit trail with Confidence Scores

---

## 5. Pricing Strategy

### 5.1 The Pricing Axis: K8s Spend, Not Developers or Clusters

**Sevro does not charge per developer.** Platform teams buy infrastructure tools; individual developers don't. Per-dev pricing (Infracost's model) fits IDE tools that developers open daily. Sevro lives in CI/CD — developers never "log in." The buyer is always the platform lead.

**Sevro does not charge pure per-cluster.** Cast AI charges $1,000/month + $5/CPU and customers call it a "savings tax that compounds." Per-cluster creates perverse incentives (merge clusters to avoid fees, monitor fewer clusters than needed). It punishes fast-moving teams that run many small environments.

**Sevro charges based on monitored Kubernetes monthly spend.** This is the axis that actually scales with customer value. A customer spending $10K/month on K8s has at most ~$3-5K/month of savings opportunity. A customer spending $500K/month has ~$150-250K/month opportunity. Same team size, same cluster count, very different value. Pricing on spend aligns our fee with their opportunity.

### 5.2 Four-Tier Model

| Tier | Price | What's Included |
|------|-------|----------------|
| **Free** | $0 forever | Unlimited PR cost-impact comments, up to 2 clusters, Confidence bands, Cost Spike detection (view-only) |
| **Team** | From $500/mo (tiered by K8s spend) | Everything in Free + Apply Fix (Deployments), Receipts, Cost Spike → PR Mapping Slack digest, 10 clusters, 90-day history |
| **Team+** | From $3,500/mo (tiered by K8s spend) | Everything in Team + Auto-Rollback Guard (notify mode), security-per-PR, ArgoCD integration, StatefulSet suggestions, unlimited clusters, 365-day history |
| **Enterprise** | $100K/yr base + 6% verified savings (capped at 2× base = $200K max) | Everything in Team+ + Auto-Rollback Guarantee with financial SLA, No-Agent Mode, SSO/SAML, SOC 2 Type 2, dedicated CSE |

### 5.3 Team Tier — K8s Spend Bands

| Monitored K8s Monthly Spend | Monthly Fee | Effective Rate |
|----------------------------|-------------|----------------|
| Up to $20K | $500 | 2.5% |
| $20K – $50K | $1,200 | 2.4% |
| $50K – $100K | $2,500 | 2.5% |

Typical K8s waste is 30–50%. A customer saving even 15% of spend gets 6× value on Sevro. CFO math is trivial.

### 5.4 Team+ Tier — K8s Spend Bands

| Monitored K8s Monthly Spend | Monthly Fee | Effective Rate |
|----------------------------|-------------|----------------|
| $100K – $200K | $3,500 | 1.75% |
| $200K – $500K | $7,500 | 1.5% |
| $500K – $1M | $14,000 | 1.4% |

Rate decreases with scale — we reward growing customers instead of taxing them. This is the primary revenue tier.

### 5.5 Pricing Principles

**Free tier is a real hook, not a trial.** Unlimited PR comments means every K8s developer feels the "see cost impact before merging" moment on Day 1. Apply Fix is the gate — they see the fix, they just can't click it. Upgrade trigger is pain, not a calendar.

**Apply Fix is the wedge between Free and Team.** The single most important product decision. Free users see their waste. Team users fix it with one click.

**Auto-Rollback lives only at Team+ and Enterprise.** This is the trust contract — it requires real infrastructure investment (7-day monitoring, statistical baselines, incident integration). We never dilute it with a "basic" version. Either full Auto-Rollback or none.

**Enterprise savings-share aligns both sides.** Customers with $5M+ annual K8s spend want shared alignment — it signals our confidence. Capped at 2× base ($200K max annually) makes it predictable for procurement.

**No hidden fees.** No per-PR charges, no per-scan charges, no per-user charges, no per-GB storage charges. K8s spend is the only variable.

**Bootstrapping new customers.** To determine a customer's K8s spend, we need CUR access. For the first 60 days, Team tier customers can bootstrap from cluster size + Prometheus node-hour estimates, then true up from CUR once access is granted.

### 5.6 Why This Beats Competitors on Pricing

| Competitor | Their Model | Our Advantage |
|------------|-------------|---------------|
| **Kubecost** (IBM) | $3.42/container-hour or ~$699+/month Business tier | We charge on value extracted, not capacity monitored. Customer saving $10K/month pays us $500 instead of $1,500+ for monitoring |
| **Cast AI** | $1,000/month + $5/CPU + % savings | We don't compound fees with CPU count. Our effective rate decreases at scale; theirs increases |
| **Infracost** | $100/seat/month | Different product (Terraform, not K8s). We don't compete here directly |
| **nOps** | Flat fee, unlimited clusters | We're more aligned than flat fees — their pricing doesn't scale with value |

### 5.7 Unit Economics (Target, Year-by-Year)

| Metric | Year 1 | Year 2 | Year 3 |
|--------|--------|--------|--------|
| Gross margin | 55–65% | 68–75% | 75–80% |
| Blended ACV | $18K | $35K | $55K |
| CAC | $6K | $8K | $10K |
| CAC payback | 5–7 months | 4–6 months | 3–5 months |
| Net Revenue Retention | 115–125% | 125–135% | 130–140% |
| Logo churn | 6–10% | 4–6% | 3–5% |

ACV is higher than the prior per-developer model because K8s-spend-based pricing captures value proportional to customer size. A $1M-K8s-spend customer on Team+ pays $168K/year (vs. ~$24K on per-dev). A $10K-K8s-spend customer pays $6K/year (vs. $3.6K on per-dev at 5 seats). Both tiers are healthier economics.

---

## 6. Go-to-Market Strategy

### 6.1 The Motion — Multi-Channel PLG, Not Conference-Dependent

**Core funnel:**
1. Platform engineer discovers Sevro via one of six channels (sandbox, CLI, HN, GitHub Marketplace, SEO, Twitter/LinkedIn, KubeCon)
2. **Sandbox mode** — pastes a Helm chart on sevro.dev, sees Apply Fix preview in 3 minutes (no signup)
3. **Installs GitHub App** on a test repo — first PR comment within 10 minutes of install
4. **Installs cluster agent** via Helm — Prometheus-grounded Confidence unlocks
5. First real Apply Fix PR lands within 24–48 hours on a real overprovisioning case
6. First verified Receipt in 30 days
7. Platform lead upgrades to Team tier when Apply Fix paywall triggers
8. Growing K8s spend triggers Team+ band upgrade (automatic, tier-based)
9. At scale, moves to Enterprise with savings-share

### 6.2 Launch Plan

**Months 0–3 — Foundation**
- Build sandbox mode (`sevro.dev/sandbox`)
- Build and publish CLI (`npx @sevro/cli`)
- Hand-pick 5 design partners from founder network
- Ship Apply Fix end-to-end on design partner's real Helm charts
- First verified Receipt delivered

**Months 3–6 — Public Launch**
- **"Show HN" launch** — single high-stakes Hacker News moment once sandbox + CLI + 3 customer Receipts are live
- Open self-serve Team tier billing
- GitHub Marketplace listing optimized
- First paying Team customer
- Target: 2,000 GitHub App installs, 50 paying teams

**Months 6–9 — KubeCon EU + SEO Engine**
- KubeCon Europe talk + booth
- Technical SEO engine: 2 deep posts/month
- KubeWeekly + CNCF newsletter placements
- Target: 5,000 sandbox uses/month, 150 paying teams

**Months 9–12 — Scale & Enterprise Pilots**
- KubeCon US, PlatformCon, FinOps X
- Launch Sevro Community (Discord)
- First "State of Kubernetes Efficiency" report
- Target: 300 paying teams, $3M ARR, 5 enterprise pilots

**Months 12–18 — Enterprise Motion**
- First Head of Sales hire (Month 13)
- SOC 2 Type 1 certified (Month 15)
- 5 enterprise logos at $100K–$250K ACV
- First "Sevro Score" quarterly reports reaching CTOs

### 6.3 Six Distribution Channels (Diversification)

Previous drafts treated KubeCon + CNCF as the primary GTM channel. That's a bet on one vector that runs three times per year with 6–9 month lead times. If KubeCon underperforms, we lose 60% of pipeline. Sevro distributes through **six channels in parallel**. No single channel accounts for more than 25% of signups by Month 12.

**Channel 1: Sandbox + CLI (Week 1)** — viral self-serve. `sevro.dev/sandbox` takes any Helm values.yaml and produces a preview in 3 minutes, shareable via `sevro.dev/r/<hash>`. The `npx @sevro/cli` CLI is one line to run. Target: 5,000 sandbox uses/month by Month 6.

**Channel 2: "Show HN" launch (Month 3)** — single high-stakes post once sandbox + CLI + 3 Receipts are live. Two engineers answering every comment for 48 hours. Dev-tool HN launches historically deliver 1,000–5,000 signups in 72 hours.

**Channel 3: GitHub Marketplace (Day 1)** — polished listing, SEO-optimized for "Kubernetes cost PR," "Helm values optimization," "K8s rightsizing." Target: top-10 in Marketplace DevOps category by Month 6.

**Channel 4: Technical SEO (compounding over 18 months)** — 2 deep posts/month targeting long-tail queries platform engineers search. Each post ranks for 3–5 queries. Target: 5K organic monthly signups by Month 12.

**Channel 5: Dev Twitter + LinkedIn (founder-led, daily)** — one visible thing per week: anonymized Receipts, Apply Fix demos, contrarian takes. LinkedIn for platform-engineering leads. Target: 10K Twitter + 5K LinkedIn by Month 12.

**Channel 6: KubeCon + CNCF community (Month 6 onward)** — still important, still real, but one of six. $150K/year conference budget. Target: 3 talks accepted per year, 500 booth conversations per KubeCon.

**Distribution moats in parallel:**
- **Sevro Community (Discord/Slack)** — free-tier users get access, peer-to-peer help. Once 1,000+ active, switching costs extend beyond features.
- **"State of Kubernetes Efficiency" annual report** — anonymized customer data, press-cited, becomes reference document. Competitors can't replicate without customer base.
- **Public Helm Chart Efficiency Rankings** — rate popular charts (ingress-nginx, postgresql). Chart maintainers care. SEO gold.
- **Operator + investor endorsements** — Kelsey Hightower-style endorsement beats any marketing.

### 6.4 Content Strategy

Every piece is engineered for two things: teaching platform engineers, and earning links from the K8s community.

- **The Receipt Report** — weekly newsletter with top verified savings (anonymized)
- **Helm Chart Teardowns** — analyses of common overprovisioning in popular public charts
- **State of Kubernetes Efficiency** — annual report with anonymized customer benchmarks
- **Platform Engineering Guides** — practical playbooks on requests/limits, HPA tuning, PDBs
- **Customer Stories** — real platform leads, real dollar figures, real Helm diffs
- **Kubecon vs Sevro** comparison posts — direct SEO for people comparison shopping

---

## 7. Product Roadmap

### 7.1 Year 1 Principle — Ruthless Kubernetes Focus

AWS EKS only. Helm + Kustomize only. ArgoCD read-only. Cost-first with basic security. Every engineering hour must improve the flagship Apply Fix experience for Helm PRs.

### 7.2 Year 1 Roadmap (Months 0–12)

**Months 0–3 — MVP**
- GitHub App + in-cluster Helm chart install
- Helm values.yaml parser (supports common templating patterns)
- Prometheus connector (via in-cluster ServiceAccount)
- 5 core detectors: overprovisioned CPU, overprovisioned memory, missing HPA, no resource limits, privileged containers
- Cost-per-PR comment bot with **Apply Fix button**
- Confidence Score v1 (rule-based — see 7.6)
- Onboard first 3 design partners

**Months 3–6 — Trust Layer**
- Receipt generation system (AWS CUR parsing, signed receipts, 30-day verification)
- Autonomous Savings PR engine (continuous cluster scans)
- Cost Spike → Pod Mapping v1 (weekly Slack digest, simple attribution)
- Kustomize overlay parser
- 10 detectors total
- Free tier + self-serve signup
- First paying Team customer

**Months 6–9 — ArgoCD + Growth**
- ArgoCD Application manifest integration
- Engineer Impact score on every PR
- 20 detectors total
- Basic security-per-PR: runAsRoot, missing network policies, privileged pods, exposed NodePorts, hostPath mounts
- First 50 paying teams
- KubeCon EU launch

**Months 9–12 — Trust & Early Enterprise**
- **Auto-Rollback Guarantee v1** (cost + OOMKilled triggers, 7-day window, manual approval required)
- Slack weekly "top savings" digest
- SSO / SAML basic
- SOC 2 Type 1 kickoff with Vanta
- 5 enterprise pilots ($25K–$75K ACV)
- Close Seed round

### 7.3 Year 2 Roadmap (Months 12–24)

**Q1 — Multi-Cloud K8s**
- GKE support (GCP)
- AKS support (Azure)
- GitLab integration
- Hard Cost Gate for K8s namespaces (policy-as-code)

**Q2 — Terraform for K8s-Adjacent Cloud**
- Terraform HCL2 parser
- AWS-specific detectors for K8s infrastructure: EKS node groups, ALBs, EBS volumes, NAT gateways
- Start selective competition with Infracost on K8s-adjacent Terraform
- SOC 2 Type 1 certification complete

**Q3 — Flux CD + Advanced Security**
- Flux CD integration (expand GitOps coverage)
- Security-per-PR at richer coverage (30+ findings)
- Confidence Score v2 (ML-based, trained on ~50K merged PRs)
- Public leaderboard + Blame & Praise system

**Q4 — Enterprise Depth**
- SOC 2 Type 2
- Advanced RBAC, audit log export, custom price books
- First $250K+ ACV deals
- Custom detector SDK (closed beta)

### 7.4 Year 3+ — The Cost + Security PR Platform

- Full Terraform / non-K8s cloud cost (head-to-head with Infracost)
- AI/LLM infrastructure cost analysis
- Carbon-per-PR (EU CSRD compliance)
- Shadow Mode (non-GitOps → GitOps migration)
- SaaS cost detectors (Snowflake, Datadog, Databricks)
- Sevro Marketplace (community-built detectors)

### 7.5 Explicitly NOT in Year 1

- Non-Kubernetes infrastructure (Terraform, pure cloud cost)
- Azure AKS, GCP GKE (Year 2)
- Fluent CD (Year 2)
- GitLab / Bitbucket (Year 2)
- AI/LLM cost (Year 3)
- Carbon / compliance / performance impact (Year 3)
- Mobile app, browser extensions

### 7.6 Confidence Score — The Cold-Start Reality

**Year 1 is qualitative, not numerical.** PR comments show "Confidence: Low / Medium / High" with the signals that produced the band. We do NOT show fake-precise percentages ("89%", "94%") in Year 1 — those numbers imply a calibration we haven't earned yet. Investors and customers see through false precision instantly.

The underlying scoring is rule-based and weighted:

| Signal | Weight | Source |
|--------|--------|--------|
| Actual usage (P95) fits proposed requests with ≥40% headroom | 0.30 | Prometheus |
| No OOMKilled events in last 60 days on this service | 0.20 | Kubernetes events |
| No SLO burn-rate alarms in last 30 days | 0.15 | Prometheus alerts |
| HPA would not trigger at proposed requests under observed load | 0.15 | HPA config + metrics |
| No critical-path dependencies (from service graph) | 0.10 | In-cluster topology |
| Change matches a vetted safe pattern (Sevro pattern library) | 0.10 | Internal curated library |

Internal raw scores map to customer-facing bands: **Low** (< 0.65), **Medium** (0.65–0.85), **High** (≥ 0.85). Only High produces an auto-opened PR. Medium queues for human review. Low is logged for learning but never acted on.

**Year 2 transition to numerical scores:** once we have 50K+ merged PRs with verified outcomes, we can calibrate real percentages ("89% — based on similar changes across 8,000 merged PRs"). Until then, qualitative bands are the honest choice.

**Hard rule:** no numerical confidence scores until we have calibration data to back them up. This is non-negotiable product discipline.

---

## 8. Competitive Strategy

### 8.1 Competitive Landscape (Honest)

The Kubernetes cost space has clear incumbents on the cluster-side. The PR-layer is effectively empty — Kubecost's one attempt has been dormant for 3 years.

| Category | Leader | What they own | What they don't do |
|----------|--------|---------------|---------------------|
| **K8s cost-per-PR attempt (abandoned)** | **Kubecost GitHub Action** (`cost-prediction-action`) | Shipped v0.1.1 in April 2023, 31 stars, 33 lifetime commits, last updated 2023 | Dormant project. Closed-source container. Does not cover Helm values.yaml, no Apply Fix, no cluster-grounded confidence. Evidence the easy version of this category doesn't earn adoption |
| **K8s cost visibility** | Kubecost (~$70K–$100K/yr ACV) | Granular allocation, multi-cluster dashboards, Prometheus-native | No PR remediation, no savings verification |
| **K8s open-source viz** | OpenCost (CNCF) | Free, community-driven, Prometheus metrics | No remediation, no PR integration, no UI beyond basic |
| **K8s autopilot** | Cast AI (~$500M ARR) | Cluster takeover, spot migration, bin-packing | Black-box (opaque to platform teams), K8s-only, scary for risk-averse orgs |
| **K8s autonomous rightsizing** | ScaleOps, Sedai | Real-time ML-based optimization | Cluster-side not PR-native, limited auditability |
| **K8s security** | Wiz, Snyk, Kubescape | Broad detection, CVE coverage, compliance | Detection only — fixes sit in Jira |
| **Terraform cost PRs** | Infracost (3,000+ customers) | Excellent Terraform PR experience, SOC 2, AutoFix, Claude Code plugins | Does not parse Helm / Kustomize / K8s manifests |
| **K8s configuration validation** | Datree, OPA, Kyverno | Policy-as-code, admission webhooks | Detection only, no cost dimension, no auto-fix |

### 8.2 The Wedge Is Real And Empty

Sevro is the first working Kubernetes PR remediation platform. To be specific about what "first" means:
- First Helm values.yaml-aware Apply Fix
- First Prometheus-grounded Confidence Score on Kubernetes PRs
- First cryptographically signed Receipt verifying K8s savings against real cloud bills
- First Auto-Rollback Guarantee monitoring merged K8s changes
- First unified cost + security analysis in a single K8s PR comment

Kubecost attempted the basic cost-comment version of this category in 2023 and let it die. That's not a threat — it's a signal. The easy version of this category doesn't earn adoption because platform teams don't need another number; they need a working fix. We are building the thing that works.

### 8.3 How We Win Against Each

**vs Kubecost (most critical)**
*"Kubecost's Action tells you the price. Sevro writes the fix, verifies the savings, and catches regressions."* Kubecost is the best-in-class dashboard plus a cost-prediction Action. We are the remediation + verification layer. A platform team running Kubecost + Sevro is better served than one running either alone. **Critical action item: approach Kubecost (IBM) leadership in Month 3 to explore a formal partnership where their Action provides cost data and ours provides Apply Fix. If they're willing to partner, it's our biggest unlock. If they plan to compete, we learn that now and move faster.**

**vs Cast AI**
*"We're what platform teams choose when they've been burned by Cast AI's black box."* Cast AI takes over clusters autonomously. Platform engineers want auditability. Every Sevro change is a reviewable PR with a Confidence Score. We don't compete on automation depth; we compete on transparency.

**vs ScaleOps / Sedai**
Same framing as Cast AI — ML-powered autopilots that live in the cluster. We live in the PR with human-in-the-loop review. Different product categories; both can coexist in a customer (ScaleOps for non-critical, Sevro for change-reviewed workloads).

**vs Infracost**
*"Infracost handles your Terraform. Sevro handles your Kubernetes."* No overlap in Year 1. In Year 2, when we expand into Terraform for K8s-adjacent resources, we differentiate on verified Receipts, Auto-Rollback, and cluster-connected analysis (Infracost explicitly does NOT connect to cloud accounts).

**vs Wiz / Snyk / Kubescape**
*"They find. We fix."* Security detection is crowded and mature. We don't try to match their rule breadth. We take their findings (or our top-5 native detections) and ship the PR that fixes them.

**vs Datree / OPA / Kyverno**
Policy-as-code is admission-time enforcement. We're PR-time analysis + remediation. Complementary, not competitive. Customers using Kyverno + Sevro get the best of both: admission-time blocking + merge-time fixing. Year 2: read existing Kyverno policies to avoid proposing fixes that would violate them.

### 8.4 Defensive Moats

1. **Prometheus-grounded Confidence Score.** Our scores use real cluster usage data. Competitors scoring PRs without cluster access cannot match this. Data moat compounds monthly.
2. **Auto-Rollback Guarantee.** Requires continuous cluster monitoring plus signal analysis plus incident integration. Not a feature — an architecture. Hard to retrofit.
3. **Receipt moat.** Verified savings against real cloud billing data. Competitors who don't have cloud access cannot issue receipts. Trust compounds with every signed receipt.
4. **Platform-engineer brand.** Once a platform team loves Sevro, switching is emotionally expensive. See: Vercel, Linear, Tailscale, HashiCorp's early days.
5. **ArgoCD integration depth.** Every customer's ArgoCD workflow becomes instrumented by Sevro. Deep integration is sticky.
6. **LLM fine-tunes on Helm values diffs.** By Year 2 we train small models on merged PR outcomes. For common patterns, our proprietary models beat general-purpose Sonnet/GPT on Helm generation at lower cost.

### 8.5 Why We Win — The Unfair Advantage

The question every investor will ask: *"Why won't Kubecost build this in 6 months?"* Six honest answers:

**1. Architectural head start that compounds.** We're building Apply Fix + Prometheus-grounded Confidence + Receipts + Auto-Rollback as one unified graph from Day 1. Kubecost built 5 years of cost monitoring and would need to bolt on remediation. Cast AI built 5 years of autopilot and would need to bolt on auditability. We're starting where they'd have to retrofit.

**2. AI-native from Day 0.** Kubecost's product was built before LLMs could generate Helm values reliably. Cast AI's optimization is rules-based by necessity — their architecture predates reliable LLM orchestration. We build with Claude Opus + Sonnet + fine-tunes as first-class primitives. Our Apply Fix quality improves every model release without customer-side changes.

**3. Speed of a small team vs. the inertia of an incumbent.** Kubecost is now part of IBM (acquired 2024). Cast AI has 200+ employees. Both have roadmap review cycles, product councils, and legacy architecture. A 3-person startup ships Apply Fix v1 in 90 days; a 200-person company spends 90 days deciding whether to staff the project. By the time they move, we're incumbents in the PR-remediation category.

**4. Unique founding insight.** *[The CTO/co-founder backgrounds go here — currently the #1 recruiting priority. Ideal: ex-Kubecost, ex-Cast AI, ex-Weaveworks, ex-Red Hat OpenShift, or ex-CNCF maintainer. Someone who saw firsthand how platform teams hated existing tools and knows exactly what PR-remediation should feel like because they wished it existed. Without this, answers 1-3-5-6 are just claims. With this, they're credible.]*

**5. No legacy brand to protect.** Kubecost launching Apply Fix risks their "we're just monitoring, trust us" positioning. Cast AI launching "transparent PR mode" undermines their "let us handle it" pitch. Both have brand constraints we don't. We can say things they can't — like "your cluster autopilot has been lying to you" — and be believed.

**6. Distribution advantage as builders, not vendors.** We speak the language of senior platform engineers because we are senior platform engineers. Kubecost speaks the language of enterprise FinOps buyers. Different audiences, different credibility. The K8s PR-remediation buyer is the former.

The one that matters most is **#4**. Items 1, 2, 3, 5, 6 are structural — they're true of any small team building against incumbents in this space. Item 4 is what makes us specifically the team that wins. **This is why K8s-expert co-founder recruitment is the #1 execution priority — without it, the unfair advantage collapses to "we're faster than IBM," which isn't enough.**

---

## 9. Team and Hiring Plan

### 9.1 Founding Team (Month 0)

| Role | Responsibilities |
|------|------------------|
| **CEO / Founder** | Vision, fundraising, GTM, design partner relationships, founder-led sales |
| **CTO / Co-founder** | System architecture, knowledge graph, Helm parser, cluster ingestion |
| **Founding Engineer / Co-founder** | Agent engine, LLM orchestration, Apply Fix flow, Receipt engine |

Ideally, at least one founder has strong Kubernetes experience (ex-Kubecost, Cast AI, Weaveworks, HashiCorp, Datadog Cluster Agent, or equivalent). K8s credibility is critical for the Year-1 CNCF community play.

### 9.2 Seed Stage Hires (Months 6–18)

| Role | Month | Why |
|------|-------|-----|
| Backend Engineer | 6 | Helm/Kustomize parser depth, Prometheus integration |
| Developer Advocate | 9 | KubeCon content, CNCF Slack presence, blog engine |
| Backend Engineer #2 | 11 | Auto-Rollback Guard, security detectors |
| Customer Engineer | 13 | White-glove enterprise pilots and platform-team support |
| Security / Compliance Lead | 15 | SOC 2 Type 2 path, enterprise compliance |
| Product Designer | 17 | Brand polish, docs UX, KubeCon booth materials |

**End of Month 18 headcount:** 9 people.

### 9.3 Series A Hires (Months 18–30)

| Role | Month | Why |
|------|-------|-----|
| Head of Sales | 18 | Enterprise motion, only after $1M ARR self-serve |
| 2× Account Executives | 20–22 | Enterprise inbound |
| Head of Marketing | 21 | Analyst relations, KubeCon sponsorship, category building |
| Customer Success Lead | 22 | Retention and expansion |
| 3× Software Engineers | 20–26 | Multi-cloud K8s, Terraform, LLM cost features |
| SRE / Platform Engineer | 24 | On-call scale-up |

**End of Month 30 headcount:** ~18–20 people.

### 9.4 Hiring Principles

- Technical founders make every engineering hire until headcount hits 12
- First non-eng hire is Developer Advocate, not a salesperson — our GTM is community-first
- First sales hire only after $1M ARR is self-serve
- Remote-first from Day 1, timezone overlap UTC−5 to UTC+5
- First 10 hires compensated aggressively in equity (0.5–2%)
- Kubernetes experience strongly preferred in the first 6 hires

---

## 10. Fundraising Plan

### 10.1 Pre-Seed — $1.5M (Month 0)

- **Lead:** Operator angels from K8s / FinOps / dev tools (ex-HashiCorp, ex-Datadog, ex-Anthropic, ex-CNCF projects)
- **Use of funds:** Build MVP, hire founding engineer, onboard first 10 design partners
- **Dilution:** ~10%
- **Milestones to unlock seed:** 10 design partners live with verified Receipts, 3+ public testimonials

### 10.2 Seed — $5–7M (Month 6)

- **Lead:** Tier-1 seed fund with dev tools thesis (Index, Haystack, Craft, First Round, Unusual)
- **Use of funds:** Team to 9, KubeCon EU launch, hit $400K ARR, SOC 2 Type 1 kickoff
- **Dilution:** ~18–20%
- **Milestones to unlock Series A:** $1M+ ARR, 300+ paying teams, NRR >120%, clear enterprise pilot traction

### 10.3 Series A — $15–25M (Month 18)

- **Lead:** Tier-1 venture firm (Accel, Bessemer, Greylock, Sequoia)
- **Use of funds:** Build enterprise GTM, geographic expansion, SOC 2 Type 2, Year 2 multi-cloud K8s
- **Dilution:** ~20%
- **Milestones to unlock Series B:** $10M ARR, 20+ enterprise customers, multi-cloud K8s GA

### 10.4 Series B — $40M+ (Month 30–36)

- **Lead:** Growth-stage firm
- **Use of funds:** International expansion, Terraform cost expansion (Year 2→3 transition), strategic M&A if warranted

### 10.5 Strategic Investors to Target

- Anthropic Ventures (LLM alignment for Helm generation)
- CRV, Sapphire Ventures (known DevOps/infra backers)
- Operators from Kubecost, HashiCorp, Vercel, Datadog, Cast AI (yes — even competitors' alumni)

---

## 11. Financial Model Overview

### 11.1 Funnel-Based Revenue (Year 1)

| Stage | Conversion | Cumulative |
|-------|-----------|------------|
| GitHub Marketplace install | 100% | 100% |
| Activated (Prometheus connected + first PR analyzed in 7 days) | 40% | 40% |
| First Apply Fix click attempt (blocked on Free) | 25% of activated | 10% |
| First Receipt viewed (teams with 2-cluster limit hit) | 70% of click-attempts | 7% |
| Converted to Team tier paid | 35% of Receipt-viewers | 2.5% |
| Expanded to Team+ (Year 2+) | 25% of Team at 12 months | — |
| Expanded to Enterprise (Year 2+) | 10% of Team+ at 18 months | — |

**Math:** 8,000 installs in Year 1 → ~200 paying Team customers. Blended Team ACV is ~$12K (mix of bands, mostly small: $500-$1,200/mo starters) → **~$2.4M Year-1 ARR** from pure self-serve Team tier.

Add design partners converting to Team+ (~10 customers × ~$60K = $600K) and founder-led enterprise pilots (~3 × $120K = $360K), and we reach **$3.0-3.5M ARR base case Year 1**.

This is materially higher than the prior per-developer model's ~$1.5M projection because K8s-spend pricing captures value proportional to customer size from Day 1. A typical customer's K8s spend grows 30-50% YoY, which means NRR expands automatically without any upsell motion.

### 11.2 Revenue Projections

Note: ACVs are meaningfully higher than the prior per-developer model because K8s-spend-based pricing captures value proportional to customer size.

**Bear case** (Cast AI or Infracost ships a competing PR mode in Year 2):

| Year | Paid Customers | Avg ACV | Total ARR |
|------|---------------|---------|-----------|
| 1 | 150 | $10,000 | $1.5M |
| 2 | 500 | $18,000 | $9M |
| 3 | 1,400 | $28,000 | $39M |
| 4 | 2,800 | $38,000 | $106M |
| 5 | 4,800 | $48,000 | $230M |

**Base case:**

| Year | Paid Customers | Avg ACV | Total ARR |
|------|---------------|---------|-----------|
| 1 | 220 | $14,000 | $3M |
| 2 | 900 | $24,000 | $22M |
| 3 | 2,500 | $38,000 | $95M |
| 4 | 5,000 | $52,000 | $260M |
| 5 | 8,500 | $62,000 | $527M |

**Bull case** (we define the K8s PR-remediation category and land 2–3 enterprise flagships early):

| Year | Paid Customers | Avg ACV | Total ARR |
|------|---------------|---------|-----------|
| 1 | 350 | $18,000 | $6M |
| 2 | 2,000 | $30,000 | $60M |
| 3 | 5,500 | $45,000 | $248M |
| 4 | 12,000 | $60,000 | $720M |
| 5 | 19,000 | $75,000 | $1.4B |

### 11.3 Revenue Mix (Year 5, Base Case)

| Source | Share of ARR |
|--------|-------------|
| Team tier (small & mid K8s spend customers) | 25% |
| Team+ (primary revenue engine, $100K-$1M K8s spend bands) | 40% |
| Enterprise (base fee + savings share) | 30% |
| Terraform + LLM cost add-ons (Year 2+) | 5% |

### 11.4 Cost Structure (Year 3 Target)

| Category | % of revenue |
|----------|-------------|
| Cost of revenue (LLM + cloud infra + payment processing) | 22–28% |
| Research & development | 32% |
| Sales & marketing | 28% |
| General & administrative | 10% |
| **Operating margin** | **2–8% (healthy breakeven)** |

Year 5 operating margin target: 18–25% as scale leverage kicks in, fine-tuned models reduce LLM costs, and Enterprise savings-share compounds on existing customer base.

### 11.5 Cash and Burn

| Stage | Raise | Burn (monthly) | Runway | Milestones |
|-------|-------|----------------|--------|------------|
| Pre-seed | $1.5M | $75K | 18–20 months | 10 design partners, verified receipts |
| Seed | $5–7M | $280K | 18–22 months | $1.5M ARR, Team tier self-serve, SOC 2 Type 1, KubeCon booth presence |
| Series A | $15–25M | $950K | 24–30 months | $10M ARR, enterprise logos, multi-cloud K8s |

**Conference & community budget (critical for K8s-first GTM):**

| Event | Cost | Stage |
|-------|------|-------|
| KubeCon EU (sponsored booth + 2 speakers) | $50-70K | Seed |
| KubeCon US (sponsored booth + speakers) | $60-80K | Seed |
| PlatformCon | $15-25K | Seed |
| FinOps X + CNCF co-marketing | $20-30K | Seed |
| KubeCon APAC | $30-40K | Series A |
| **Annual conference budget Year 2** | **~$150K** | — |

This is materially more than typical dev-tool budgets because the K8s community is so conference-concentrated. Underfunding conferences kills the K8s-first GTM.

### 11.6 Pricing Addition: No-Agent Mode (Enterprise Security Unlock)

For customers whose security teams block in-cluster agent installs (financial services, healthcare, regulated industries), we offer a no-agent deployment:

| Mode | Data Richness | Install Effort | Availability |
|------|---------------|----------------|-------------|
| **Full Agent** (default) | 100% — rich Prometheus + events + pod metrics | Helm chart, ~10 min | All tiers |
| **No-Agent Mode** | ~60% — external kubeconfig + Prometheus remote-read only | API access only, zero in-cluster footprint | Team+ and Enterprise only (adds 25% premium to Team+ pricing) |

No-Agent Mode is a deliberate Year 1 feature, not a future roadmap item. Many enterprise deals are won on this differentiator alone. The 25% premium reflects the additional engineering complexity of running without rich signals.

### 11.6 Key Metrics to Report (Investor-Facing)

- **ARR and ARR growth** — monthly
- **NRR and GRR** — monthly, cohort-by-cohort
- **CAC Payback** — quarterly, by channel
- **Gross Margin** — monthly, target 65% by Year 2, 75% by Year 3
- **Rule of 40** — quarterly
- **Verified Savings per Customer per Month** — the trust and retention metric
- **Receipt Accuracy (predicted vs actual)** — target within 15% variance
- **Confidence Score Calibration** — does our "90% confident" score actually ship clean 90% of the time?
- **Auto-Rollback False-Positive Rate** — target <5%
- **LLM cost per merged PR** — target $0.25 or lower

---

## 12. Risk Management

### 12.1 Top Risks and Mitigations

**1. Founder / K8s-credibility gap at launch.**
The whole Year-1 GTM rests on KubeCon and CNCF community credibility. A founding team without a K8s-expert co-founder will struggle to get accepted for KubeCon talks, earn trust in CNCF Slack, or close platform-team design partners. This is the single most material risk — not a competitor, but our own bench.
- *Mitigation (priority):* K8s-expert co-founder or first technical hire is the #1 recruiting requirement. Ideal backgrounds: ex-Kubecost, ex-Cast AI, ex-Weaveworks, ex-Red Hat OpenShift, ex-HashiCorp Consul/Nomad, ex-CNCF maintainer.
- *Mitigation:* Developer Advocate hire by Month 9 to scale community presence.
- *Mitigation:* investor-advisors with PLG-to-enterprise transition experience (ex-Datadog, ex-HashiCorp, ex-Cast AI operators).

**2. Cast AI or ScaleOps launches a "transparent PR mode."**
Cast AI has $500M+ ARR and huge engineering. ScaleOps ranks first in independent K8s cost benchmarks. Either could ship a PR-based interface on top of their existing autopilot.
- *Mitigation:* platform-engineer brand — they're perceived as black-box autopilots; we're perceived as trustable PR-based. Hard brand to reverse.
- *Mitigation:* go open-source on key parts (Helm parser, Confidence Score rule library) to earn CNCF community loyalty they can't replicate.
- *Mitigation:* speed — 300+ paying K8s teams and Auto-Rollback shipped before they could credibly pivot.

**3. A bad Apply Fix causes an OOMKilled production outage.**
A single public incident ("Sevro rightsizing crashed our payment service") would be catastrophic.
- *Mitigation (product):* 40% headroom minimum on all recommendations. Never auto-open PRs with Confidence below High band. StatefulSets excluded from auto-Apply-Fix entirely in Year 1. Operator-managed workloads skipped with clear explanation.
- *Mitigation (trust):* Auto-Rollback Guarantee catches the breach within 5 minutes and notifies on-call.
- *Mitigation (legal):* Enterprise contracts include capped financial remedy ($100K or 1 month fees). Cyber liability insurance ($5M coverage) from Month 6.

**4. LLM costs erode gross margin below 50%.**
- *Mitigation:* aggressive caching (40%+ hit rate target), model routing (Haiku for classification, Sonnet for generation), batched inference.
- *Mitigation:* fine-tune small open-weight models on Helm values by Month 18 for common 80% of patterns.
- *Mitigation:* committed-use discounts with Anthropic / OpenAI at scale.

**5. Infracost expands into Helm/Kustomize parsing.**
They have brand, SOC 2, and 3,000+ customers. Helm parsing is a real engineering project but not impossible.
- *Mitigation:* speed — 300+ paying K8s teams before they could ship a credible Helm parser.
- *Mitigation:* differentiate on cluster-connected Confidence Score (Infracost explicitly does NOT connect to cloud accounts — a 12-month architectural change for them).

**6. Enterprise security review blocks in-cluster agent install.**
Installing a third-party agent in regulated clusters can require 3-month security reviews. This is a meaningful blocker for financial services, healthcare, and regulated-industry customers.
- *Mitigation (product):* offer a "no-agent mode" in Year 1 that works via external kubeconfig access only — slower data, less rich signals, but zero in-cluster footprint.
- *Mitigation (documentation):* publish agent resource footprint, signed container images (Sigstore), complete source-available auditability, SBOMs.
- *Mitigation (enterprise):* Year 2 "customer-hosted" deployment mode where Sevro runs inside the customer VPC.
- *Mitigation (pricing):* no-agent tier available at Team+ price point so enterprise-cautious customers can still buy without full trust upfront.

**7. Kubecost (IBM) revives or replaces their abandoned Action.**
Their `cost-prediction-action` has been dormant at v0.1.1 since April 2023 (31 stars). Reviving it or launching something fresh under IBM would require reversing a 3-year strategic decision, but IBM has the resources to do so if they see us gaining share.
- *Mitigation:* ship the full stack (Apply Fix + Receipts + Auto-Rollback) by Month 4 so any revival plays catch-up.
- *Mitigation:* explore a Kubecost partnership where their cost-allocation engine feeds our Apply Fix — removes their motivation to compete.
- *Mitigation:* moats that require architectural commitment, not just engineering time (cluster-connected Confidence, signed Receipts, Auto-Rollback Guarantee).

**8. Security breach of Sevro exposes customer cluster data.**
- *Mitigation:* zero-trust architecture, per-customer encryption keys, no stored credentials (STS + ServiceAccount tokens only), SOC 2 Type 2 by Month 21.

**9. Regulatory risk — GDPR, EU data residency.**
K8s manifests and Prometheus labels can contain PII. EU customers require data residency.
- *Mitigation:* compliance advisor engaged Month 3. PII detection and redaction in the ingestion pipeline by Month 6. EU region deployment Month 18.

**10. Vendor concentration (GitHub, Anthropic, AWS).**
- *Mitigation:* multi-LLM abstraction Day 1 (Anthropic primary, OpenAI fallback, open-weight tertiary).
- *Mitigation:* GitLab roadmap Year 2 for VCS diversification.
- *Mitigation:* multi-cloud K8s support (GKE, AKS) Year 2 for customer-side diversification.

### 12.2 Scenario Planning

| Scenario | Trigger | Response |
|----------|---------|----------|
| **Bear** | $400K ARR at Month 18 | Narrow scope further, extend runway, pivot to deeper Kubecost integration |
| **Base** | $1–2M ARR at Month 18 | Execute Series A plan as designed |
| **Bull** | $4M+ ARR at Month 18 | Raise opportunistic insider round, accelerate enterprise hiring by 6 months |

---

## 13. The First 90 Days (Operating Plan)

### 13.1 Days 1–30 — Foundation
- Lock co-founders, sign founder agreements
- Incorporate Delaware C-Corp, set up cap table, banking
- Close pre-seed commitments
- Ship "Hello World" GitHub App + in-cluster ServiceAccount proof-of-concept
- Core engineering infra: CI/CD, observability, on-call
- Dogfood from Day 1: run Sevro on the company's own EKS cluster

### 13.2 Days 31–60 — Product v0.1
- Helm values.yaml parser (common templating patterns)
- Prometheus connector
- First 3 detectors (overprovisioned CPU, overprovisioned memory, missing limits)
- **Apply Fix flow end-to-end** — flagship interaction, must be flawless
- Confidence Score v1 (rule-based)
- Onboard first design partner (founder's network)

### 13.3 Days 61–90 — Design Partners + Trust Layer
- Onboard 3 external design partners with real production EKS clusters
- Ship PR comment bot with Apply Fix, Confidence Score, Engineer Impact
- Ship Slack integration (PR notifications + approval flow)
- Ship Receipt Engine v1 (AWS CUR parsing, manual verification initially)
- First verified Receipt delivered to Design Partner #1
- Ship Cost Spike → Pod Mapping v1 (weekly Slack digest)
- Record first demo video for fundraising

### 13.4 Success Criteria (Day 90)
- 3 design partners actively using Sevro with Apply Fix on real Helm PRs
- First verified Receipt ($500+/mo confirmed against real AWS bill)
- Apply Fix flow works end-to-end without manual intervention
- Confidence Score displayed on every PR with real Prometheus supporting data
- 2 testimonials captured on video
- Seed fundraising deck ready with customer logos
- Clear technical roadmap to ship Auto-Rollback Guarantee v1 by Month 9

---

## 14. Open-Source Discipline — What We Open, What We Protect

Open source is a tool, not a religion. Every public repository is a business decision with a cost and an expected return. This section lays out exactly what Sevro makes public, what stays private, and the business reasoning behind each choice.

**The governing principle:** open-source only what earns us something specific (credibility, enterprise trust, community leverage) AND does not erode our competitive moat. Everything else stays private.

### 14.1 What We Make Public — With Business Rationale

**Year 1: One public repository.** `github.com/sevro/agent` — Apache 2.0.

| What | License | Business Reason |
|------|---------|-----------------|
| **In-cluster agent** (Go binary + Helm chart + RBAC manifests) | Apache 2.0 | **Unlocks enterprise deals.** Regulated industries (finance, healthcare, government) will not install closed-source binaries in production clusters. Our competitive analysis shows 30-40% of target enterprise customers require source-auditable agents. This single decision unlocks $10M+ in Year-2 enterprise ARR. Also gives CNCF community a concrete "Sevro is one of us" signal. |

**That's it for Year 1.** Not the parser. Not the CLI. Not the Confidence Score rules. Not the detector definitions. One repository, one carefully scoped artifact.

**Year 2: Two more repositories — only if specific pressure justifies them.**

| What | License | Business Reason (and trigger condition) |
|------|---------|-----------------------------------------|
| **Helm parser** (submitted to CNCF Sandbox as `Sevro-helm-parser`) | Apache 2.0 | **Only if** we've accumulated >50 chart-specific bug reports we can't keep up with, AND a motivated external contributor appears. CNCF Sandbox submission is a massive credibility signal, but only worth it if the maintenance burden is truly shared. |
| **Cost Prediction schema & methodology** (published as docs + reference implementation) | Apache 2.0 | **Only if** regulatory pressure (CSRD, FinOps Foundation standards) requires auditable methodology. Opens our math to scrutiny without opening our code. Trust win for enterprise. |

**Never open-sourced (explicit list):**

| What | Why It Stays Private |
|------|----------------------|
| Apply Fix generation pipeline | **This is the company.** LLM orchestration + validation + sandboxing = 12+ months of engineering. Copy-proof value. |
| Confidence Score scoring engine and weights | **Proprietary trust signal.** Platform engineers trust the bands precisely because we calibrate them against our customer base. Open-sourcing the weights invites gaming. |
| Auto-Rollback Guard statistical baselines | **Trust moat.** Box-Cox transforms, STL decomposition, threshold calibration — required customer data to build. Competitors can copy the code; they can't copy the tuned thresholds. |
| Receipt Engine (CUR parsing + signing + verification) | **Integrity anchor.** Receipts work because customers trust our signed methodology. Open-sourcing invites fake receipts or cloned verification that dilutes our brand. |
| Cost Spike → PR Mapping algorithms | **Unique IP.** Causal attribution is novel engineering. Kubecost doesn't have this; we keep the lead. |
| Detector SDK internals (launch public in Year 2 as a USAGE SDK, not the core engine) | **Platform play.** Year 2 we publish the detector API surface so customers/partners can write detectors. The engine that runs them stays private. |
| Apply Fix Template Marketplace engine (Year 3) | **Curation is the product.** Patterns are community-contributed; the curation, quality scoring, and distribution platform is ours. |
| All cross-customer data pipelines and aggregation logic | **The biggest moat.** Differential privacy, benchmark computation, anonymization. This is the Datadog-style compounding moat. Cannot be open. |
| All LLM prompts, prompt templates, prompt caching strategies | **Prompt engineering is not a durable moat generally, BUT published prompts accelerate competitors by 3-6 months.** We don't volunteer the shortcut. |
| All training data and fine-tuned model weights (Year 2+) | **Data advantage.** 50K+ merged Sevro PRs with verified outcomes = our proprietary training set. |
| SaaS backend (API, dashboards, billing, multi-tenancy, auth) | **This is the service.** Competitors can't replicate operational reliability. |

### 14.2 Why "One Public Repo" Is The Right Answer

Three alternatives were considered and rejected:

**Rejected Alternative A: Full open source (HashiCorp/Docker model).**
- Upside: maximum community goodwill, viral adoption
- Downside: category is crowded with free tools (OpenCost, Goldilocks, KubeVela). We don't win by being "more free" — we lose because everything becomes forkable. Cast AI or IBM/Kubecost forks our full stack, rebrands, and undercuts us on hosting. **Rejection reason: direct path to commoditization.**

**Rejected Alternative B: Fully closed (traditional SaaS).**
- Upside: maximum moat preservation, no maintenance burden
- Downside: enterprise security teams block agent install (kills 30-40% of target market). CNCF community treats us as a vendor, not a member. Can't get KubeCon talks accepted. **Rejection reason: blocks both enterprise segment AND community credibility.**

**Rejected Alternative C: Open-core with generous open tier (GitLab/Supabase model).**
- Upside: community trust, community contributions
- Downside: the generous open tier directly competes with our paid product. Free users never convert because free is already good enough. Maintenance burden on multiple repos diverts engineering from the proprietary product. **Rejection reason: economically weaker than a focused one-repo model for this specific category.**

**Chosen: Minimal open-source discipline.** One repo that unlocks the enterprise market (the agent). Everything else private. No apologies, no "we'll open-source more in the future" promises, no maintenance drift.

### 14.3 Business Outcomes of This Discipline

**What we gain:**
- 30-40% of enterprise market becomes addressable (source-auditable agent requirement satisfied)
- CNCF-community "we're one of you" signal delivered through a concrete artifact
- Zero competitive risk from forking — the valuable parts are unforkable
- Minimal maintenance burden — one repo is manageable by a single engineer
- Clear marketing message: *"Agent is open. Platform is commercial. Our pricing matches our value."*

**What we avoid:**
- Competitors forking our parser and undercutting us
- Open-source community expecting us to maintain components that don't serve us
- "Fair source" or "BSL" arguments that signal commercial weakness
- The maintenance tax of 5+ public repositories for a 3-person engineering team
- Accidental over-disclosure as the team grows

### 14.4 Internal Access Control (Protecting the Private Parts)

Not all engineers need access to all proprietary code. From Day 1:

| Role | Access Scope |
|------|--------------|
| Founding engineers (3) | Everything |
| Platform/infra hires (Months 6-12) | Backend + infra, no LLM fine-tunes or customer training data |
| Developer Advocate | Public repo only + docs + customer-visible APIs |
| Customer-facing engineers (Year 2) | All except training data and prompt templates |
| Contractors / agency partners | Per-repo scoped time-bounded access, no SaaS backend |
| External OSS contributors | Agent repo issues + PRs only |

Signed NDA mandatory for all hires. IP assignment clauses in every contract. Trade secret designation for prompts, Confidence weights, Auto-Rollback thresholds, and training data. This is legally meaningful — trade secrets lose protection when disclosed, even accidentally.

### 14.5 Public Communication Rules

When asked publicly (at KubeCon, on podcasts, in investor meetings, in sales calls) whether Sevro is open source, the team gives a consistent answer:

> *"The cluster agent is open source under Apache 2.0 — it's what runs in your cluster and we believe you have the right to audit anything running in production. The Sevro platform itself is a commercial SaaS; that's what we charge for. We think this is the same split HashiCorp uses with Terraform Cloud and Kubecost uses with OpenCost — open where it matters for trust, commercial where it matters for business."*

Confident, principled, clear. No apology for the commercial parts. No promises we don't intend to keep.

### 14.6 Decision Framework for Future Open-Sourcing Requests

When customers, community members, or team members propose open-sourcing something new, require a written one-pager answering:

1. **What specific business outcome does opening this earn?** (enterprise unlock? category leadership? community goodwill? CNCF credibility?)
2. **What's the maintenance cost?** (engineer-hours per month, forever)
3. **What competitive advantage do we lose?** (if any — be specific)
4. **Is there an alternative that earns the same benefit without opening it?** (e.g., published methodology docs instead of code)
5. **Who champions it internally, and will they own the ongoing burden?**

The default answer to open-source proposals is **no**. The burden of proof is on the proposer. This is the opposite of the industry-default "we should open-source by default" stance — and it's the right stance for a category-defining company building against well-funded incumbents.

### 14.7 Competitive Benchmarks — What the Leaders Actually Do

For context, here's the open-source posture of the companies we aspire to match in category dominance:

| Company | Open Source | Commercial | Ratio Open:Commercial |
|---------|-------------|------------|----------------------|
| **Datadog** | Minimal (OpenMetrics contributions only) | Everything else | ~5% : 95% |
| **HashiCorp** (pre-BSL) | Terraform, Vault, Consul, Nomad | Cloud + Enterprise tiers | ~50% : 50% |
| **MongoDB** | MongoDB Community | Atlas + Enterprise | ~60% : 40% |
| **Snowflake** | Minimal | Everything | ~2% : 98% |
| **Stripe** | SDKs only | Platform | ~10% : 90% |
| **Kubecost** | OpenCost (CNCF) | Kubecost Enterprise | ~30% : 70% |
| **Cast AI** | Nothing meaningful | Everything | ~5% : 95% |
| **Wiz** | Nothing | Everything | 0% : 100% |

**Observation:** the companies with the highest category dominance (Datadog, Snowflake, Stripe, Wiz) are all low-open-source. HashiCorp and MongoDB, which were more open, both eventually tightened via BSL and other licensing changes as they scaled — and both faced meaningful fork pressure (OpenTofu, Amazon DocumentDB). **We learn from their experience and default closed.**

Our target ratio: ~10% open, 90% commercial. Close to the Stripe / Datadog side of the spectrum.

---

## 15. Platform Strategy — Becoming the Integration Surface

Products get acquired. Platforms define industries. The distinction matters because our goal is to dominate Kubernetes — and domination requires being the surface that every other K8s tool plugs into, not a feature inside someone else's platform.

### 15.1 The Three Platform Primitives

We build these in Year 1 (even if closed) so they're battle-tested when we open them in Year 2. Every architectural decision in `technical_implementation.md` assumes these will be public APIs eventually.

**Primitive 1: Sevro API (private Year 1, public Year 2)**
Every action Sevro takes — open a PR, score confidence, compute cost delta, issue a receipt, trigger rollback — has an internal API call behind it. In Year 2 we expose these with rate limits, API keys, and SDKs in Go, TypeScript, and Python. Customers build internal dashboards on our API. Partners build integrations. Consultants build white-label tools.

**Primitive 2: Detector SDK (public Year 2)**
External security researchers, FinOps consultants, platform teams, and customers write custom detectors that plug into Sevro. A detector is a Go module that implements `Detect(workload) → []Finding`. Sevro runs community detectors alongside built-in ones. Good detectors get featured. Great detectors become built-in.

**Primitive 3: Apply Fix Template Marketplace (public Year 3)**
Community-contributed fix patterns for common K8s waste. *"ingress-nginx rightsizing for medium-traffic services"* becomes a one-click template. Contributors get attribution. Downloaders get vetted patterns. Sevro curates quality. This is the GitHub Actions Marketplace model applied to K8s remediation.

### 15.2 Partnership Program (Year 2 Launch)

The partnership program makes Sevro indispensable by routing other tools' value through us:

| Partner | What they send Sevro | What Sevro sends them |
|---------|--------------------------|---------------------------|
| **Wiz / Snyk / Aqua** | K8s security findings | Apply Fix PRs that close the findings |
| **Datadog / New Relic / Grafana** | SLO breach signals | Auto-Rollback triggers correlated to their alerts |
| **PagerDuty / Opsgenie** | Incident context | Rollback-to-incident correlation data |
| **Kubecost / OpenCost** | Cost-allocation data | PR-time cost analysis powered by their attribution |
| **AWS / GCP / Azure Marketplaces** | Billing integration, marketplace listing | Native deploy of Sevro via cloud marketplace |

Each partnership either (a) makes Sevro the remediation surface for their findings — which makes us the stickier product — or (b) removes their incentive to compete because they're getting distribution through us.

**Program mechanics:**
- **Technical integration support** from our Partner Engineer (Month 18 hire)
- **Joint customer case studies** quarterly
- **Co-marketing** at KubeCon, joint webinars, partner blog features
- **Revenue share** for deals originated through partner motion
- **Certification program** for consulting partners by Year 3

### 15.3 API Ecosystem — What Gets Built On Sevro

Once the API is public, these are the kinds of integrations that emerge organically — each one deepens our moat:

- **Internal platform portals** at large enterprises that pull Sevro data into custom dashboards
- **FinOps consulting tools** that use Sevro as their delivery mechanism
- **CI/CD plugins** (Circle, GitLab, Jenkins) that run Sevro checks before merge
- **Backstage plugins** exposing Sevro Score and receipts in the developer portal
- **Terraform providers** managing Sevro configuration as code
- **Slack / Teams bots** beyond our own digest
- **Custom detectors for industry-specific compliance** (HIPAA, PCI, FedRAMP)

Every one of these = another reason a customer doesn't leave Sevro. Platform dependencies compound. Single-product dependencies erode.

### 15.4 "Built on Sevro" Badge

Year 3: we launch the formal "Built on Sevro" certification for third-party tools. Earning the badge requires:
- API integration passing our conformance tests
- Documented Sevro data usage with customer consent
- Listed in our integration directory
- Co-marketing commitment

This is the CNCF "sandbox project" model applied to our partner ecosystem. It converts one-off integrations into permanent dependencies.

---

## 16. Mission Preservation — Building to Last, Not to Flip

At some point, Sevro will be courted by Datadog ($50B+), GitHub/Microsoft, HashiCorp/IBM, Palo Alto Networks, or a sovereign fund. The right strategy depends on the founders' answer to one question: **do you want to be acquired into a feature, or define an industry?**

This section exists because investors and future hires will ask. Having an explicit answer protects the mission.

### 16.1 Three Inflection Points Where Mission Preservation Matters

**Inflection 1: Seed → Series A ($1.5M → $15M)**
Signal: successful seed close, early traction. Risk: strategic early acquirer offers $50-80M. Decision rule: **reject below $200M**. At seed stage, Sevro is worth more as a category-defining company than as a bolt-on to an incumbent.

**Inflection 2: Series A → Series B ($15M → $50M)**
Signal: $10M+ ARR, 200+ customers. Risk: Datadog or Wiz offers $300-500M. Decision rule: **reject below $1B**. At this stage we've proven category definition; an acquisition makes us a feature inside someone else's roadmap. Unless the offer is life-changing for founders AND mission-preserving (rare), decline.

**Inflection 3: Series C+ ($50M+ raised)**
Signal: $50M+ ARR, 2,000+ customers, category leadership clear. Risk: a sovereign fund, Palo Alto, or IBM offers $3-5B. Decision rule: **case-by-case, but default to IPO path**. At this scale, public markets typically value Sevro at multiples that beat strategic acquirers. IPO preserves mission AND maximizes outcome.

### 16.2 Founder Equity Targets (Mission-Preservation Floors)

To preserve mission through Series B, founders should target:

| Stage | Combined Founder Ownership | Strategy Implication |
|-------|----------------------------|----------------------|
| Pre-seed | 85-90% | Standard |
| Post-seed | 65-75% | Preserve optionality |
| Post-Series A | 45-55% | Still controlling enough to reject bad offers |
| Post-Series B | 30-40% | Board composition matters more than equity |
| Post-Series C | 20-30% | Dual-class shares if IPO path emerges |

**Hard rules:**
- No investor gets a board seat that can force a sale before Series B
- Anti-acquisition provisions in Series A+ term sheets (founders' approval required for any acquisition under $500M)
- Dual-class share structure considered at Series B if mission-critical
- Founder-friendly clauses: no forced founder removal without cause, extended cliff periods

### 16.3 Who We Specifically Want to Not Be Acquired By

Some acquirers preserve mission. Others kill it. Our explicit list:

| Acquirer | If Acquired, What Happens | Position |
|----------|---------------------------|----------|
| **Datadog** | Sevro becomes a K8s feature in their platform, non-Datadog customers lose access within 2 years | ⚠️ Decline at any reasonable price |
| **Cisco / AppDynamics / Splunk** | Sevro sunset within 3 years; enterprise-only | ⛔ Decline |
| **Microsoft / GitHub** | Could work if Sevro stays independent inside (Mojaloop / Copilot model) | 🟢 Possible at IPO-equivalent valuation |
| **Palo Alto Networks** | Security-only positioning; cost story dies | ⛔ Decline |
| **IBM** (Kubecost's parent) | Direct category collapse; mission extinguished | ⛔ Decline at any price |
| **HashiCorp** (now IBM) | Same | ⛔ Decline |
| **Google Cloud** | Could work if Sevro stays multi-cloud (rare in Google acquisitions) | 🟡 Conditional |
| **AWS** | Better: they've kept MongoDB-competitor acquisitions relatively independent | 🟡 Conditional |

### 16.4 The IPO Path

Year 5-6 target: $500M+ ARR, 30%+ growth, 80%+ gross margins. These metrics support a $5B-10B IPO in the 2030-2031 window.

Comparable exits:
- HashiCorp IPO (2021): $14B — later acquired by IBM for $6.4B
- GitLab IPO (2021): $11B
- Datadog IPO (2019): $10B, now $40B+
- Snowflake IPO (2020): $70B
- MongoDB IPO (2017): $2B, now $20B+

Our path looks most like MongoDB's: category definition, enterprise + self-serve motion, open-source-adjacent, multi-cloud. Pattern is: IPO at $5B, scale to $20B over 5 years post-IPO.

### 16.5 Cultural Mission Preservation

Mission isn't just about who owns the company — it's about who builds it. Values we hire for and protect:

- **Engineering-led product decisions.** PMs serve engineers, not the other way around.
- **Honesty with customers over growth at any cost.** We tell customers when our tool isn't the right fit.
- **Compounding reputation over quick wins.** A bad Receipt or a false-positive Auto-Rollback hurts us more than any missed quarter.
- **Kubernetes community first.** Even when it slows us down. Even when it costs marketing budget.

Hire for these. Fire fast when they're violated. Compound over a decade.

---

## 17. Anti-Commoditization Moats

In 3 years, LLMs will be cheaper, better, and table stakes. Every DevOps startup will have "AI agents that write PRs." Our differentiation against today's incumbents (Kubecost, Cast AI) becomes undifferentiation against tomorrow's startups. This section identifies what survives commoditization and what doesn't.

### 17.1 What Erodes (Don't Depend On It)

**❌ "We use LLMs for Helm generation"** — Table stakes by 2027. Every K8s tool will claim this.

**❌ "We have a Confidence Score"** — Every competitor will have one within 12 months of seeing ours.

**❌ "We have nice PR comments"** — UI quality is easy to copy.

**❌ "We have Kubernetes context"** — client-go and Prometheus are public APIs. Anyone can build this.

**❌ "Our prompts are better"** — Prompts leak (via reverse engineering, via team churn, via published research). Prompt engineering is not a durable moat.

### 17.2 What Compounds (Invest Heavily In These)

**✅ Customer data moat (strongest).** Five years of anonymized cross-customer Helm chart outcomes, verified receipts, Auto-Rollback false-positive feedback. A competitor entering in 2028 cannot replicate this. Every merged Sevro PR deepens this moat permanently. **This is the Datadog moat, the Stripe moat, the MongoDB moat.**

**✅ Trust brand (slow to build, slow to lose).** After 5 years of signed Receipts with verifiable accuracy, after thousands of Auto-Rollback events correctly detected, Sevro has the trust reputation that Cast AI spent 5 years failing to earn. New entrants can't shortcut trust.

**✅ Distribution advantage.** 50K+ newsletter subscribers, 5K+ Discord community, 4-person DevRel team, annual Sevro Summit, CNCF maintainers on staff — these are hard to fake and hard to build quickly.

**✅ Integration depth.** Once Wiz, Datadog, PagerDuty, Kubecost, and every major K8s tool integrate with Sevro, switching us out = disconnecting from all of them. Year 3: ~20 major integrations. Year 5: 100+. Exit cost for customer grows exponentially.

**✅ Compliance certifications.** SOC 2 Type 2 (Month 21), FedRAMP Moderate (Year 3), HIPAA BAA (Year 2), ISO 27001, PCI-DSS. Each certification unlocks a customer segment competitors can't reach without 18+ months of audit work.

**✅ Regulatory moat (Year 3+).** As K8s cost and security reporting becomes regulated (CSRD carbon reporting, enterprise FinOps governance), Sevro is the verification-ready tool. New entrants face regulatory lead time we've already paid.

### 17.3 The 2028 Competitive Test

If commoditization hits in 2028 and every startup ships "AI K8s rightsizing," the question becomes: **why Sevro specifically?**

Answer framework:
1. "Because we've been doing this longer than anyone." (Trust + data moat)
2. "Because our data — 5 years of cross-customer receipts — produces better fixes than theirs." (Data moat)
3. "Because every K8s tool you use already routes through us." (Integration depth)
4. "Because your auditor accepts Sevro Receipts." (Compliance + regulatory)
5. "Because switching off means disconnecting from your Wiz, Datadog, and PagerDuty workflows." (Integration lock-in)

None of these are features. All of them are compounding advantages. A new entrant with a better product still faces all five simultaneously.

### 17.4 What This Means for Year-1 Investment

Priorities shift if we're building for post-commoditization durability:

| Investment | Year-1 Weight | Rationale |
|------------|--------------|-----------|
| Feature breadth | Lower than you'd think | Features commoditize |
| Cross-customer data pipeline | **Highest priority** | The compounding moat |
| Trust reputation (receipts, low FP rate) | **Very high** | Long cycle to build, permanent value |
| CNCF community presence | High | Distribution advantage |
| Integration depth with 3-5 key partners | High | Early lock-in |
| Compliance prep (SOC 2 Type 1 kickoff) | High | Blocks enterprise segments |
| Shiny UI | Low | Commoditizes fast |

Every engineering hour asks: does this compound, or does this erode? Compound work gets priority.

---

## 18. Revenue Projections — Domination Trajectory

### 18.1 Why the Prior Projections Were Too Conservative

Section 11's projections (pre-recalibration) ($95M Year-3 base, $527M Year-5 base) are calibrated for "build a good company." For "dominate Kubernetes," these numbers should be the floor, not the ceiling.

Domination requires meaningful market penetration. The TAM test: CNCF survey identifies ~30,000 organizations running K8s in production with GitOps workflows. To dominate, Sevro needs:

- **Year 3: 25%+ penetration** = 7,500+ paying customers (vs. prior base case of 2,500)
- **Year 5: 40%+ penetration** = 12,000+ paying customers (vs. prior base case of 8,500)

The prior "base case" (8% penetration at Year 3) is a respectable business but not a dominant one. We reframe below.

### 18.2 Revised Projections — Domination as Base Case

| Year | Paid Customers | Avg ACV | Total ARR | K8s-GitOps Penetration |
|------|---------------|---------|-----------|------------------------|
| 1 | 300 | $16,000 | $4.8M | 1% |
| 2 | 1,800 | $26,000 | $47M | 6% |
| 3 | 5,500 | $42,000 | $231M | 18% |
| 4 | 11,000 | $56,000 | $616M | 37% |
| 5 | 18,000 | $72,000 | $1.3B | 60% |

This is the domination trajectory. Not aggressive; required.

### 18.3 What This Requires (Honestly)

A domination trajectory is not achievable on a Seed → Series A → small Series B plan. It requires:

**Capital plan (revised):**
- Pre-seed: $1.5-2M (unchanged)
- Seed: $8-12M (was $5-7M — larger because of DevRel team + community investment)
- Series A: $30-50M (was $15-25M — larger because of enterprise sales buildout + multi-cloud expansion)
- Series B: $100-150M (Month 30-36)
- Series C: $200-300M (Month 48, for international expansion)

**Team size (revised):**
- Year 1: 8 people (was 6)
- Year 2: 35 people (was 22)
- Year 3: 120 people (was 60)
- Year 5: 400+ people

**DevRel investment (revised):**
- 4-person team by Month 18 (was 1 DA)
- $800K/year DevRel budget by Month 18
- Annual Sevro Summit budget ($2M by Year 3)
- CNCF Platinum membership Year 3 ($150K/year)

**GTM investment (revised):**
- VP Sales hire Month 12 (was Month 18)
- Enterprise AE team of 6 by Month 24
- Customer Success team of 8 by Month 24
- Solutions Engineering team of 4 by Month 24

### 18.4 The Investor Conversation

When pitching domination-trajectory, the investor conversation changes:

**Old pitch:** "$95M ARR Year 3, 3x growth, profitable Year 4."
→ Tier-2 VC language. Good returns, not category-defining.

**New pitch:** "Category-defining K8s cost-and-safety platform. $230M ARR Year 3 with 18% penetration of a $3B TAM growing 35% YoY. Dominant position enables $1B+ ARR Year 5 and $10B+ IPO Year 6."
→ Tier-1 VC language. Aligns with how Accel, Bessemer, Greylock, Sequoia think about infrastructure category leaders.

Both pitches require the same product, same team, same technical execution. The difference is ambition, scope, and capital plan. **Dominating requires acting dominantly from Day 1.**

### 18.5 The Floor Case (If Domination Fails)

If we execute well but don't achieve domination (e.g., Cast AI pivots aggressively, or Kubecost revives), we fall back to the previous "base case":

- Year 3: 2,500 customers, $95M ARR
- Year 5: 8,500 customers, $527M ARR

This is still a billion-dollar outcome. The floor case of the domination plan is the ceiling case of the modest plan. Planning for domination means planning for large upside; if we miss, we still win.

---

## 19. The Closing Argument

The Kubernetes cost market is crowded. Kubecost has enterprise logos. Cast AI runs $500M+ in revenue. OpenCost is CNCF-incubated. Infracost owns Terraform. But **none of them sit inside the Helm PR with Apply Fix.**

That's the empty category. That's where we win.

Year 1, we execute on exactly one thing: be the best tool a platform engineer has ever used for reviewing Helm and Kustomize pull requests. Ship Apply Fix flawlessly. Ship Receipts with <15% prediction error. Ship Auto-Rollback Guarantee as a real product guarantee, not marketing copy. Close 10 design partners, 300 paying teams, $1.5M ARR.

Year 2, we expand outward into the cloud resources Kubernetes runs on — the door opens for us to compete with Infracost from an established K8s position. Multi-cloud K8s. Terraform for K8s-adjacent cloud resources. SOC 2 Type 2 and enterprise.

Year 3, we become the cost and security PR layer for every modern engineering team. AI/LLM costs. Carbon reporting. Shadow Mode for non-GitOps shops. The full platform.

The wedge is narrow. The moat is deep. The timing is exact.

Now we ship.

---

*Document version: 4.1 · Added Section 14 (Open-Source Discipline) with business-oriented decisions on what stays public vs private · April 2026*
