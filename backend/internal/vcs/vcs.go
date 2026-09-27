// Package vcs defines the pluggable source-control contract.
//
// GitHub lands first (Phase 4); GitLab follows in Phase 8 with the
// same interface so webhook receivers, PR/MR comment renderers, and
// signed-token Apply Fix flows compose against one shape.
package vcs

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Provider names a supported source-control system. New providers MUST
// be added to the receipts.vcs CHECK constraint and CHECK in apply_fixes.
type Provider string

const (
	ProviderGitHub    Provider = "github"
	ProviderGitLab    Provider = "gitlab"
	ProviderBitbucket Provider = "bitbucket"
)

// Repo identifies a repository inside a provider.
type Repo struct {
	Provider Provider
	Owner    string
	Name     string
}

// String renders Provider:Owner/Name.
func (r Repo) String() string { return fmt.Sprintf("%s:%s/%s", r.Provider, r.Owner, r.Name) }

// PullRequest is the normalised view of a PR / MR.
type PullRequest struct {
	Repo       Repo
	Number     int
	URL        string
	Title      string
	BaseBranch string
	HeadBranch string
	Author     string
	State      PRState
}

// PRState mirrors the apply_fixes.state enum in the schema.
type PRState string

const (
	PRStateOpen       PRState = "open"
	PRStateMerged     PRState = "merged"
	PRStateClosed     PRState = "closed"
	PRStateRolledBack PRState = "rolled-back"
)

// Comment is the body posted under a PR. CommentID is provider-assigned
// after creation; callers store it so we can edit instead of re-posting.
type Comment struct {
	ID   string
	Body string
}

// OpenPRRequest tells a provider to open a PR/MR with an Apply Fix diff.
type OpenPRRequest struct {
	Repo       Repo
	BaseBranch string
	HeadBranch string
	Title      string
	Body       string
	// FilePatches keyed by repo-relative path; raw new file contents.
	FilePatches map[string][]byte
	Labels      []string
}

// Provider is what every source-control implementation satisfies.
type Source interface {
	Provider() Provider

	// VerifyWebhook authenticates an inbound webhook. Implementations
	// validate the provider-specific signature header (`X-Hub-Signature-256`
	// for GitHub, `X-Gitlab-Token` for GitLab).
	VerifyWebhook(secret []byte, signatureHeader string, body []byte) error

	// PostComment creates or updates a comment on a PR.
	PostComment(ctx context.Context, pr PullRequest, c Comment) (Comment, error)

	// OpenPR opens a PR/MR with the supplied diff. Returns the
	// provider-assigned PR number and URL.
	OpenPR(ctx context.Context, req OpenPRRequest) (PullRequest, error)
}

// Registry holds the registered providers. One per process via Default.
type Registry struct {
	mu      sync.RWMutex
	sources map[Provider]Source
}

func newRegistry() *Registry { return &Registry{sources: map[Provider]Source{}} }

var defaultRegistry = newRegistry()

// Default returns the process-wide registry.
func Default() *Registry { return defaultRegistry }

// Register adds a source. Duplicates panic.
func (r *Registry) Register(s Source) {
	if s == nil || s.Provider() == "" {
		panic("vcs: nil source or empty provider")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sources[s.Provider()]; dup {
		panic("vcs: duplicate provider " + s.Provider())
	}
	r.sources[s.Provider()] = s
}

// Lookup returns the registered source for a provider.
func (r *Registry) Lookup(p Provider) (Source, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[p]
	if !ok {
		return nil, fmt.Errorf("vcs: provider %q not registered", p)
	}
	return s, nil
}

// Providers returns the registered providers in deterministic order.
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.sources))
	for p := range r.sources {
		out = append(out, p)
	}
	sortProviders(out)
	return out
}

// ErrNotImplemented is returned by stub sources whose features are
// scheduled for a later phase.
var ErrNotImplemented = errors.New("vcs: not implemented in this phase")

// ErrInvalidSignature is returned by VerifyWebhook when the supplied
// signature does not match the body under the provided secret.
var ErrInvalidSignature = errors.New("vcs: invalid webhook signature")

func sortProviders(s []Provider) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
