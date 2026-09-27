// Package tenancy is the isolation primitive: RLS-scoped DB
// connections, per-tenant Temporal task queues, Redis key prefixes, S3
// path scoping.
//
// Every domain package's public method takes *tenancy.Context as the
// first arg after context.Context. CLAUDE.md "Multi-tenancy is
// non-negotiable" — RLS enforcement on the DB side assumes it.
package tenancy
