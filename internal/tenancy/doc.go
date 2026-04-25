// Package tenancy enforces multi-tenant isolation: tenant-scoped DB
// connections (RLS), per-tenant Temporal task queues, Redis key prefixes,
// and S3 path scoping.
//
// Every domain package's public API takes a *tenancy.Context as the first
// argument after context.Context.
package tenancy
