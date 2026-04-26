# Sevro Data Processing Addendum — Template

> **Status:** template draft only. Review by qualified counsel required before any
> customer execution. The Phase 5 onboarding flow signs this via DocuSign;
> until then it is provided here as a publicly-auditable artefact.
> Last reviewed: 2026-04-27.

This Data Processing Addendum (the "DPA") forms part of the Master
Services Agreement (the "Agreement") between **Sevro, Inc.** ("Sevro")
and the customer identified in the Order Form ("Customer"), and
governs the processing of Personal Data by Sevro on Customer's behalf
in connection with Customer's use of the Sevro platform.

## 1. Definitions

- **Applicable Data Protection Law** — the GDPR, the UK GDPR, the
  CCPA/CPRA, and any other data-protection law applicable to Customer's
  Personal Data.
- **Personal Data**, **Processing**, **Controller**, **Processor**,
  **Data Subject** — as defined in the GDPR.
- **Customer Personal Data** — Personal Data that Customer transmits
  to, or generates through, the Sevro platform.
- **Subprocessor** — any third party engaged by Sevro to Process
  Customer Personal Data, as listed in
  [SUBPROCESSORS.md](SUBPROCESSORS.md).

## 2. Roles and scope

Customer is the Controller and Sevro is the Processor of Customer
Personal Data. The categories of data, categories of Data Subjects,
nature and purpose of Processing, and duration are described in
**Annex I** below.

## 3. Sevro obligations as Processor

Sevro shall:

1. Process Customer Personal Data only on documented instructions
   from Customer, including the instructions in the Agreement and
   this DPA.
2. Ensure that personnel authorised to process Customer Personal
   Data are bound by confidentiality obligations.
3. Implement appropriate technical and organisational security
   measures (Annex II), including:
   - Multi-tenant isolation enforced at the database row level
     (PostgreSQL Row-Level Security keyed on tenant identifier)
   - Encryption at rest (AWS KMS) and in transit (TLS 1.2+)
   - Per-tenant Receipt-signing keys held in HSM (AWS KMS asymmetric)
   - Annual penetration testing
   - SOC 2 Type 1 (in motion; expected Month 9), Type 2 (Year 2)
4. Assist Customer in responding to Data Subject Access Requests
   (DSARs) — see Section 5.
5. Notify Customer without undue delay (and within 72 hours) upon
   becoming aware of a Personal Data breach.
6. Make available to Customer the information necessary to
   demonstrate compliance and submit to audits and inspections
   (Section 7).

## 4. Subprocessors

1. Customer authorises Sevro to engage Subprocessors as listed in
   [SUBPROCESSORS.md](SUBPROCESSORS.md).
2. Sevro shall maintain a current list and provide **30 days' prior
   written notice** of any new Subprocessor.
3. Customer may object to a new Subprocessor within 30 days. If the
   parties cannot reach agreement, Customer may terminate the
   affected services with a pro-rated refund.
4. Sevro remains liable for the acts and omissions of its
   Subprocessors as if they were Sevro's own.

## 5. Data Subject Access Requests (DSARs)

Sevro shall, taking into account the nature of the Processing,
assist Customer by appropriate technical and organisational measures
in fulfilling its obligation to respond to DSARs.

Customers may invoke programmatic DSAR endpoints at any time:

- `GET /api/v1/dsar/export` — produces a signed ZIP archive of
  Customer Personal Data for a Data Subject.
- `DELETE /api/v1/dsar/erase` — schedules erasure of a Data
  Subject's data with a 30-day purge window; tombstone records are
  retained only for audit purposes.

## 6. Data retention

Sevro retains Customer Personal Data for the following periods,
enforced by automated retention jobs:

| Data category | Retention period |
| --- | --- |
| Prometheus snapshots | 90 days |
| LLM call logs (input/output hashes, model, cost) | 30 days |
| Verified Receipts | 7 years (financial records) |
| Tenant audit log | 7 years |
| Customer Personal Data after subscription end | 30 days, then erased; backups purged within a further 60 days |

## 7. Audits

1. Sevro provides a SOC 2 report annually to qualifying customers
   under NDA.
2. Customer may request an audit of Sevro's compliance with this DPA
   no more than once per twelve-month period, on at least 60 days'
   notice, conducted at Customer's expense.
3. The audit shall be confidential and shall not unreasonably
   interfere with Sevro's business operations.

## 8. International data transfers

When Customer Personal Data is transferred from the European Economic
Area, the United Kingdom, or Switzerland to a country not deemed
adequate by the European Commission, the transfer is governed by the
Standard Contractual Clauses (Module Two: Controller to Processor)
incorporated by reference into this DPA.

EU-resident tenants are processed in `eu-west-1` exclusively from
Phase 8 (Month 9 of Year 1). Until then, EU tenants are processed in
`us-east-1` under the Standard Contractual Clauses; Customer
acknowledges this in the Order Form.

## 9. Term

This DPA takes effect on the Effective Date of the Agreement and
continues until termination of the Agreement, except that Sections 3,
6, and 7 survive termination for the duration of any continuing
Processing of Customer Personal Data.

---

## Annex I — Description of Processing

| Item | Description |
| --- | --- |
| Subject matter | Provision of the Sevro platform |
| Duration | Term of the Agreement |
| Nature and purpose | Cost and security analysis of Customer's Helm charts and Kubernetes clusters |
| Categories of Data Subjects | Customer's developers, platform engineers, and administrators authorised to use Sevro |
| Categories of Personal Data | Email addresses (for authentication), GitHub/GitLab usernames, IP addresses (request logs), audit-log actor identifiers |
| Special categories | None. Sevro does not process special categories of Personal Data. |

## Annex II — Technical and Organisational Measures

Sevro maintains the following measures, as further described in our
SOC 2 report:

- **Access control** — single sign-on with hardware MFA for all Sevro
  personnel; least-privilege IAM; just-in-time elevation
- **Encryption** — TLS 1.2+ in transit; AWS KMS (AES-256-GCM) at rest
- **Multi-tenant isolation** — PostgreSQL Row-Level Security on every
  tenant-scoped table; per-tenant Temporal task queues; per-tenant
  Redis key prefixes; per-tenant S3 prefixes
- **Logging and monitoring** — centralised structured logging with
  tenant-scoped retention; SIEM alerting on suspicious access patterns
- **Vulnerability management** — Trivy + CodeQL + gosec + govulncheck
  on every build; quarterly dependency reviews; annual third-party
  penetration tests
- **Incident response** — documented runbook; on-call rotation;
  72-hour breach notification SLA
- **Backups and disaster recovery** — RDS PITR (35-day retention);
  cross-region snapshot replication; quarterly fire-drill restore tests

---

**Signed for Customer**

Name: __________________________
Title: __________________________
Date: __________________________

**Signed for Sevro**

Name: __________________________
Title: __________________________
Date: __________________________
