// Package ratelimit gates the public sandbox surface. The Limiter
// interface lets cmd/api swap a Memory limiter (Phase 2) for a Redis
// limiter (Phase 5+, when ElastiCache binds) without changing handlers
// or routes. The default KeyFn is the client IP; a fingerprint
// extractor composes into the same signature.
package ratelimit
