// Package db wires the tenant-scoped Postgres bind (via the
// `app.tenant_id` GUC, load-bearing — RLS reads it) and Redis keyspace
// helpers. Every Redis key MUST go through Keyspace; a direct
// redis.Client call outside this wrapper is a P0 tenancy bug.
package db
