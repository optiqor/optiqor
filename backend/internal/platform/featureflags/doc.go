// Package featureflags wraps the OpenFeature client (backed by
// self-hosted Unleash in prod) with a tenant-aware EvalContext. Phase 1
// ships an in-process Provider; the Unleash adapter swaps in via Set()
// at boot without touching call sites.
package featureflags
