// Package cluster ships the production K8s implementations of the
// readers declared in internal/agent/k8s. Backed by client-go shared
// informer caches — list/watch the apiserver once, serve every reader
// from the in-memory index. Used only by cmd/agent; the SaaS API and
// worker bind the in-memory fakes from internal/agent/k8s so backend
// tests never need a real apiserver.
//
// ADR-0008 RBAC: every reader maps to a single
// {get,list,watch} verb on one GVR. Adding a reader widens the agent's
// RBAC envelope and needs an ADR update.
package cluster
