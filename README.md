# backend

Sevro backend monorepo. Go modular monolith producing three binaries from `cmd/`.

## Binaries

| Binary | Purpose |
| --- | --- |
| `api` | HTTP API + GitHub App webhook receiver + sandbox endpoints |
| `worker` | Temporal worker (LLM orchestration, PR writes, Receipts, Auto-Rollback monitor) |
| `agent` | In-cluster Kubernetes agent (deployed to customer clusters via Helm; reads K8s API + Prometheus, ships data over mTLS to SaaS) |

## Quickstart

```sh
# 1. Install local toolchain
make install-tools

# 2. Bring up Postgres + Redis + Temporal locally
make docker-up

# 3. Apply migrations + seed dev data
make migrate
make seed

# 4. Run the API in dev mode
make run-api
```

## Layout

```
cmd/             # binary entrypoints (api, worker, agent)
internal/        # domain packages (ingestion, parser, agent, cost, ...) + platform/
web/             # sandbox + dashboard frontend (framework TBD Phase 2)
migrations/      # goose SQL migrations + RLS policies
infra/terraform/ # AWS infra as code
deploy/          # our own Helm charts + ArgoCD Applications
scripts/         # dev helpers
tests/           # integration + e2e (real Postgres via testcontainers)
docs/adr/        # architectural decision records
```

## Conventions

See [CLAUDE.md](CLAUDE.md) for layering rules, multi-tenancy invariants, and testing standards.

## License

Proprietary. See [LICENSE](LICENSE). The in-cluster agent (`cmd/agent`) is the **exception** — it ships under Apache 2.0 from Sevro, because regulated customers will not run closed-source binaries inside production clusters. See [LICENSE-agent](LICENSE-agent).
