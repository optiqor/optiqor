# ADR-0011: Receipt signing via KMS, never in-process keys

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Security

## Context

The cryptographically signed receipt is Optiqor's foundational trust artifact. A receipt cryptographically attests: *"Optiqor recommended action X on date Y, customer merged it, 30 days later the actual savings were Z, verified against this exact set of CUR rows whose hash is W."* Third parties (auditors, customer's CFO, journalists) can verify a receipt independently using Optiqor's published signing keys without needing access to Optiqor's systems.

This trust property depends entirely on **key custody**. If Optiqor's signing keys could ever be exfiltrated or used by an Optiqor employee in an unauthorized way, the entire moat collapses. A receipt is only as trustworthy as the chain of custody for the key that signed it.

There are exactly three ways to manage signing keys:

1. **Keys in application memory.** Easy. Vulnerable. A memory dump, a debug log, a compromised employee with production access — all can leak keys.
2. **Keys in a secrets manager (Vault, AWS Secrets Manager).** Better. But application code still fetches the key value, holds it in memory, and signs with it. Same fundamental vulnerability surface.
3. **Keys in a KMS that signs without releasing the key material.** The application makes a "sign this" API call to the KMS; the KMS signs and returns the signature; the key material never leaves the KMS. **This is the only architecture that genuinely protects the key.**

This is also a compliance question. SOC 2, FedRAMP, and PCI all distinguish between "you have access controls" (good) and "the key material itself is protected by an HSM-equivalent" (much better).

## Decision

**All receipt signing happens via a cloud KMS (initially AWS KMS).** The signing key material is generated in KMS and never leaves KMS. Application code calls KMS with a payload and a key identifier; KMS returns the signature.

Specifically:

- **Key algorithm:** Ed25519 (small signatures, fast verification, well-supported).
- **Key generation:** done in KMS at infrastructure setup time; the private key material is never exported.
- **Per-region keys:** US keys for US tenants, EU keys for EU tenants (data residency).
- **Key versioning:** new keys are generated periodically; all keys remain valid for verification forever. Each receipt records which key version signed it.
- **Public keys:** published on `optiqor.dev/keys/` with version, valid-from date, and signing algorithm.
- **Signing logs:** every KMS sign operation generates a CloudTrail entry. Optiqor cannot make a signature without leaving an audit log entry. This is the property that makes "Optiqor cannot forge a receipt" structurally true.
- **Signer service isolation — deployment, not codebase.** Per ADR-0005, the monorepo produces a single binary with mode flags. The signer is `cmd/api --mode=signer` (or equivalent) deployed as a **separate Kubernetes workload with a dedicated ServiceAccount and IAM role**. The mode boundary is enforced at the deployment + IAM layer, not by code separation:
   - **IAM scope:** the signer SA's role has *only* `kms:Sign` on Optiqor's signing key alias, *only* `s3:PutObject` to the receipt prefix in the receipts bucket, *only* `INSERT` on the transparency log table. No access to `customer_*` tables, no access to the cluster API, no AWS Secrets Manager read.
   - **Network scope:** signer workload egress is restricted (NetworkPolicy + security group) to KMS, S3, and the Postgres primary — nothing else. No general internet, no LLM provider, no GitHub.
   - **Code discipline:** the signer mode's entry-point package must not transitively import non-signer business logic (no `internal/prwriter`, no `internal/agent`, no `internal/cost`). The dependency graph keeps the signer small and reviewable. A CI lint check (`go list -deps`) enforces this in the same commit that wires the signer mode.
   - **Why deployment-level not repo-level:** SOC 2 cares about the runtime isolation (separate IAM, separate audit log, bounded blast radius) — not whether the source code lives in one repo. Sharing the codebase keeps the canonical-JSON encoder and the receipt schema definition single-source-of-truth, which matters more for correctness than repo separation does for security.

**Application code never holds the signing key material in memory.** This is enforced by IAM: the application's IAM role has `kms:Sign` permission on the signing key, not `kms:GetPublicKey`-as-private or any export operation. KMS itself refuses to export Ed25519 private keys; this is a guarantee from AWS.

## Alternatives considered

**Alternative 1: Keys in environment variables or config files.**
Easy. Rejected. Catastrophically insecure; a single misconfigured log statement could leak the key.

**Alternative 2: Keys in Vault or AWS Secrets Manager, fetched at application startup.**
Better, but application code still holds the key in memory after fetching. Rejected because: a memory dump, a compromised production node, or a compromised employee with prod access all defeat this. KMS provides a structurally stronger guarantee — the key material *cannot* be retrieved.

**Alternative 3: HSM (Hardware Security Module) instead of cloud KMS.**
The strongest option. Effectively the same security guarantee as KMS but customer-side hardware. Rejected for now because: AWS KMS already provides HSM-backed key storage under the hood (FIPS 140-2 Level 3 validated). Self-managed HSM is operationally heavy and unnecessary for our scale. Reconsider for FedRAMP-tier deployment.

**Alternative 4: Per-customer signing keys (each tenant gets their own KMS key).**
Stronger isolation: a key compromise affects one customer, not all. Rejected for Year 1 because: operational complexity (managing thousands of KMS keys, tracking which customer maps to which key); receipt verification becomes per-customer, not universal. **Reconsider in Year 2 for enterprise-tier customers who require it.**

**Alternative 5: Signer as a separate repository (own binary, own CI, own deploy pipeline).**
Earlier drafts of this ADR implied a fully separate signing service in its own codebase. Rejected because: the canonical-JSON encoder, the receipt schema, and the verification logic all need to be single-source-of-truth across the API tier (which writes receipt payloads), the worker tier (which orchestrates the 30-day verification workflow), and the signer (which signs the final artifact). Splitting the repo would force the schema to be duplicated or vendored across two codebases — both options invite drift. The runtime-isolation properties SOC 2 cares about are delivered by separate deployment + IAM + network scope (see "Signer service isolation" above), not by repo separation.

## Consequences

**Easier:**
- "Optiqor cannot forge a receipt" is structurally true. An employee with full database access still cannot mint a fake signature — the math requires the private key, which is in KMS, behind IAM-controlled `kms:Sign` calls that all generate audit logs.
- SOC 2 audit conversation about key custody is straightforward: KMS, audit logs, IAM scopes documented.
- Key rotation is supported by design (every receipt records its key version; new keys can be issued without invalidating old receipts).
- The signer service has a tightly bounded blast radius: even if compromised, the attacker can sign receipts but can't read customer data or modify the K8s API.

**Harder:**
- Each receipt requires a KMS API call. Latency: typically 50-100ms; throughput: thousands of calls per second per region (well above our needs). Cost: small per-signature fee, negligible at our volume.
- KMS is a single point of failure within a region. If AWS KMS has an outage, we cannot sign new receipts. **Mitigation: receipts are time-shifted (we sign 30+ days after the change). A KMS outage of hours or even a day doesn't lose data; the signing job retries. Multi-region failover is a Year 2 hardening project.**
- Cloud lock-in: our signing infrastructure is tied to AWS KMS. If we ever needed to use a different cloud, we'd need to migrate keys (which requires generating new keys; old receipts still verify against the old AWS-KMS public keys). Acceptable lock-in for the security gain.

**Locked into:**
- Ed25519 as the signing algorithm. Switching would mean every customer-side verification tool needs to update. **The signing algorithm is part of the methodology spec and is versioned with it.**
- KMS as the key custody mechanism. Reversing this would require a security review and a new ADR. We are committing to "signing keys never leave KMS" indefinitely.
- The transparency log mechanism: append-only Merkle-tree-based, similar to Sigstore's Rekor. This is what makes individual receipts verifiable as part of a publicly auditable record.

**When we'd revisit this:**
- Per-customer keys for enterprise tier (Year 2).
- Multi-region failover for the signing service (Year 2-3).
- HSM upgrade path for FedRAMP-tier customers (Year 3+).
- A move to a different KMS provider, only if forced (e.g., by a customer requirement that AWS KMS cannot satisfy).

## Open questions

- The exact transparency log implementation. Lean toward a Merkle-tree-based design with periodic root hashes published. Specifically: do we build our own or integrate with Sigstore's Rekor? Year 1 implementation choice; defer for now.
- The key rotation cadence. Lean toward: rotate annually as standard practice, immediately on suspected compromise.
- Customer-facing verification tooling: a CLI (`optiqor verify receipt.json`) plus a web UI on `optiqor.dev/verify/<receipt-id>`. Defer specifics.

## Implementation status

**Partial.** Ed25519 signing + canonical-JSON encoder shipped in `internal/receipts/receipts.go` + `internal/receipts/canonical.go` with round-trip tests. Public verification endpoint `GET /v1/receipts/{id}` shipped in `internal/receipts/handler.go`. **Not yet shipped:** KMS swap (currently uses process-local `ed25519` keys; KMS `kms:Sign` integration is Phase-6 work per `optiqor/todo.md:340`), transparency log (`internal/receipts/tlog`, Phase 6 per `optiqor/todo.md:342`), separate-deployment signer mode with dedicated SA/IAM/NetworkPolicy, WebCrypto browser verifier, and `@optiqor/verify` CLI.

*Last verified: 2026-05-18.*
