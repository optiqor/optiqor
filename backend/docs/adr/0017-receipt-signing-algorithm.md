## ADR-0017: Receipt signing — switch from Ed25519 to ECDSA P-256

**Status:** Accepted
**Date:** 2026-05-24
**Domain:** Security
**Supersedes (partially):** ADR-0011 §"Key algorithm" only

## Context

ADR-0011 mandated that the receipt-signing private key never leave the KMS that holds it. The same ADR picked Ed25519 as the algorithm — small signatures, fast verification, well-supported in modern stdlibs.

The two commitments collide. **AWS KMS does not support Ed25519 signing.** It supports ECDSA on P-256 / P-384 / P-521 secp256k1, RSA on a handful of moduli, and HMAC. It does not implement Ed25519, and there is no public roadmap commitment to add it.

The Phase-4 KMS-Sign early-kickoff task (2 days; landing in PR #20+) needs the algorithm pinned before its Terraform module for the dev KMS key can declare `customer_master_key_spec`. The wrong choice wastes those two days when the module has to be torn down and rebuilt.

Three real paths exist:

1. **Ed25519 with KMS envelope encryption (Vault pattern).** Generate an Ed25519 key, encrypt the *private* key material with a KMS data key, persist the ciphertext; on each sign decrypt → sign in memory → wipe. Pro: keeps Ed25519. Con: contradicts ADR-0011 — the private key *does* enter memory, even if only briefly. The CloudTrail audit trail becomes "decrypt this data key" not "sign with this private key," which weakens the "Optiqor cannot make a signature without leaving an audit log entry" property ADR-0011 leans on.

2. **Ed25519 via AWS CloudHSM.** True HSM-backed Ed25519. Pro: keeps Ed25519 + the keys-never-leave guarantee. Con: CloudHSM is single-region, $1.45/hr/HSM-instance, requires a customer-managed cluster, much more operational overhead than KMS. Wrong cost-curve for a Year-1 startup.

3. **ECDSA P-256, KMS-native.** Pro: KMS issues signatures directly, private key truly never leaves; CloudTrail logs every sign; multi-region replication is a flag on the KMS key. Con: signatures are ~64 bytes (vs Ed25519's 32 — irrelevant at our scale), signature verification is a few times slower than Ed25519 (still microseconds), the algorithm choice ties us to a NIST curve.

No third-party receipt verifier tooling has shipped yet, so the cost of changing the algorithm now is roughly zero. The cost of changing it in a year (when external auditors are running their own verifiers) is much higher.

## Decision

**Switch the receipt-signing algorithm from Ed25519 to ECDSA P-256.** The rest of ADR-0011 (KMS-only, no in-process keys, per-region keys, public keys at `optiqor.dev/keys/`, CloudTrail-logged signing, signer-mode deployment isolation) stands.

Specifically:

- **Algorithm:** ECDSA on secp256r1 (NIST P-256), SHA-256 hash.
- **KMS key spec:** `customer_master_key_spec = "ECC_NIST_P256"`, `key_usage = "SIGN_VERIFY"`.
- **Signature encoding:** DER-encoded ASN.1 SEQUENCE of two INTEGERs (r, s). The wire format keeps the existing `sig.payload` shape from `internal/receipts/receipts.go`; only the algorithm changes.
- **Verifier expectations:** `crypto/ecdsa` for Go callers, `Web Crypto API` (`ECDSA` with `P-256` + `SHA-256`) for browser-side verification in the dashboard, OpenSSL `ec` for shell verifiers.
- **Migration:** the existing process-local `ed25519.PrivateKey` Issuer stays around (tests, dev). Production wires a new `KMSSigner` implementation of the same `Issuer` interface that calls `kms:Sign` with `SigningAlgorithm = ECDSA_SHA_256`. The interface boundary lets test paths keep using Ed25519 *or* an in-process ECDSA fake without forcing every test to depend on a real KMS.
- **Key rotation:** unchanged from ADR-0011. New key versions live alongside old ones; receipts record which version signed them.

## Alternatives considered

**Stick with Ed25519 + KMS envelope encryption.** Rejected — see Context option 1. Letting the private key touch process memory at all undermines the structural property the receipt system is built on. The CloudTrail audit log is the second-best thing about KMS-only signing (after key custody), and envelope-encryption-style signing loses both.

**Stick with Ed25519 + CloudHSM.** Rejected — see Context option 2. CloudHSM operational overhead is much wider than the algorithm choice itself; the cost-of-running side of the trade is several orders of magnitude worse than the cost-of-verifying side that Ed25519 wins. Revisit at Year 3 if we acquire customers who specifically require HSM-resident keys (some EU regulated industries).

**Switch the algorithm at Year-3 instead of now.** Rejected. There is no third-party verifier tooling today. The cost of switching grows monotonically with adoption. Make the irreversible-feeling decisions while they're still reversible.

## Consequences

What gets easier:

- The Phase-4 KMS-Sign early-kickoff task ships next week without algorithm uncertainty.
- The Terraform module for the dev KMS key (`infra/modules/kms-signer/`) reduces to one `aws_kms_key` resource with `customer_master_key_spec = "ECC_NIST_P256"`.
- Multi-region key replication becomes a KMS configuration flag (already supported for ECDSA keys).
- Web-dashboard verification works out of the box with `Web Crypto API`, which supports P-256 in every modern browser without polyfills.

What gets harder:

- Signatures grow from 32 bytes (Ed25519) to ~64-72 bytes (ECDSA DER). Irrelevant at our payload sizes.
- Verification is ~3-10× slower than Ed25519. Still microseconds; not a hot-path concern.
- We commit to a NIST curve. If a future cryptanalytic attack on P-256 emerges, we rotate keys + re-sign anchored history. The transparency log makes this auditable.

## Implementation status

**Pending PR #20+** (Phase-4 KMS-Sign early kickoff). On acceptance of this ADR:

- `internal/receipts/Issuer` interface stays the same.
- `internal/receipts/StaticIssuer` (the process-local signer used today) continues with Ed25519 for backward compat in tests + dev — production swap is opt-in via config.
- A new `internal/receipts/kms.KMSSigner` calls `kms:Sign` with `ECDSA_SHA_256` and returns the DER-encoded `r,s` blob. Integration test against a real dev KMS key; unit test against a fake `kms:Sign` client.
- `infra/terraform/modules/kms-signer/` declares `ECC_NIST_P256` + `SIGN_VERIFY`.
- The transparency-log schema and the receipt wire format do **not** change. Only the algorithm identifier in `signing_key_id` reflects the switch (e.g., `optiqor-receipt-2026-q3-ecdsa-p256`).

*Last verified: 2026-05-24.*
