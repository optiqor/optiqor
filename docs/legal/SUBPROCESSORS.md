# Sevro Subprocessors

> Last reviewed: 2026-04-27 · Maintained by Sevro Security & Privacy.

This document lists every third party that processes Customer Data on
Sevro's behalf. Customers covered by a Data Processing Addendum (DPA —
see [DPA-template.md](DPA-template.md)) may subscribe to changes via
the RSS feed at `https://sevro.dev/legal/subprocessors.rss`. Material
additions are announced **30 days before activation**.

## Definitions

- **Customer Data** — any data a customer or their users submit to,
  store on, or generate through the Sevro platform: cluster metrics,
  Helm chart contents, Receipts, recommendations, audit log records.
- **Subprocessor** — any third party engaged by Sevro that may
  process Customer Data while delivering services.

## Active subprocessors

| Subprocessor | Purpose | Customer Data scope | Region | Certifications |
| --- | --- | --- | --- | --- |
| **Amazon Web Services, Inc.** (AWS) | Hosting (EKS), object storage (S3), database (RDS Postgres), key management (KMS), secrets (Secrets Manager) | All Customer Data at rest and in transit | `us-east-1` (US tenants), `eu-west-1` (EU tenants from Phase 8) | SOC 2 Type 2, ISO 27001, ISO 27018, HIPAA |
| **Anthropic, PBC** | LLM inference for Apply Fix diff generation, narrative summarisation, and `@sevro` Q&A | Sanitised and PII-minimised Helm values + recommendation context only; never customer secrets | US-region API; Anthropic EU endpoint exclusively for EU tenants | SOC 2 Type 2 |
| **Sentry (Functional Software, Inc.)** | Error monitoring | Stack traces and request metadata; redaction filters strip tenant and PII payloads | EU/US (Self-hosted in EU for EU tenants from Phase 8) | SOC 2 Type 2, ISO 27001 |
| **Stripe, Inc.** | Subscription billing, customer portal, invoicing, tax | Billing identity, plan tier, usage counters; no Helm or cluster data | US/EU | SOC 1, SOC 2 Type 2, PCI DSS L1 |
| **DocuSign, Inc.** | DPA and other contract signature flow (Phase 5) | Names + business email of signing parties only | US/EU | SOC 1, SOC 2 Type 2 |
| **GitHub, Inc.** | Source-control integration via GitHub App + OAuth | PR metadata customers route through Sevro's webhook receiver; no Sevro-stored secrets shared back | US (GitHub managed) | SOC 1, SOC 2 Type 2 |
| **GitLab, Inc.** (Phase 8) | Source-control integration | MR metadata routed through Sevro's webhook receiver | US/EU | SOC 2 Type 2, ISO 27001 |

## Pending subprocessors

The following are scheduled to be added in announced phases. The
30-day notification clock starts when each is activated; this list is
informational only.

| Subprocessor | Purpose | Phase |
| --- | --- | --- |
| **Microsoft Corporation (Azure Cost Management API)** | Azure billing data ingestion for AKS Receipts | Phase 7 |
| **Hetzner Online GmbH** | Hetzner Cloud invoice ingestion for Hetzner Receipts | Phase 8 |

## How we use subprocessors

- We sign DPAs and (where applicable) Standard Contractual Clauses
  with every subprocessor before sending them Customer Data.
- We require subprocessors to maintain confidentiality and security
  controls at least as protective as our own.
- We restrict subprocessors to the minimum data required for their
  role (data minimisation).
- We never use Customer Data to train shared LLM models. Anthropic's
  zero-data-retention configuration is enabled for all Sevro inference
  traffic.

## Customer rights

- You may request the current subprocessor list at any time via
  `legal@sevro.dev`.
- You may object to a new subprocessor within 30 days of public
  announcement. If we cannot find a mutually agreeable resolution,
  you may terminate the affected services and receive a pro-rated
  refund.

## Change history

| Date | Change |
| --- | --- |
| 2026-04-27 | Initial publication. |
