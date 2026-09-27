// Package attribution closes the loop on a merged Apply Fix PR: posts
// the realised-savings follow-up comment, marks apply_fixes.merged_at,
// and emits the tenant-scoped metric so the dashboard's savings card
// updates. Phase-4 deliverable per docs/strategy/technical_implementation.md.
package attribution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// MergedEvent is the normalised input to OnMerged. The webhook parser
// strips provider-specific fields so the handler is provider-agnostic.
type MergedEvent struct {
	Provider       string // "github" | "gitlab"
	InstallationID int64
	RepoOwner      string
	RepoName       string
	PRNumber       int
	MergedAt       time.Time
}

// Row is what Store.LookupByRepoPR returns. Cents are int64 because
// the attribution comment quotes annual figures.
type Row struct {
	ID                     string
	TenantID               string
	PRURL                  string
	Workload               string
	Title                  string
	MonthlySavingsUSDCents int64
}

// Store is the seam to apply_fixes. PgStore satisfies it; tests use
// a fake.
type Store interface {
	LookupByRepoPR(ctx context.Context, t tenancy.Context, repo string, prNumber int) (Row, error)
	MarkMerged(ctx context.Context, t tenancy.Context, id string, mergedAt time.Time) error
}

// TenantResolver maps a (provider, installation_id) pair back to the
// owning tenant. Implementations use set_superuser_context('on', reason)
// for the bypass, leaving an audit_log row per resolve.
type TenantResolver interface {
	ResolveInstallation(ctx context.Context, provider string, installationID int64) (string, error)
}

// CommentPoster wraps vcs.Source.PostComment so handlers can be tested
// without spinning a fake GitHub. Production wraps the real vcs.GitHub.
type CommentPoster interface {
	Post(ctx context.Context, t tenancy.Context, repoOwner, repoName string, prNumber int, body string) error
}

// Handler orchestrates OnMerged. Each dependency is required — a nil
// dependency is a misconfiguration bug, not a runtime fallback.
type Handler struct {
	Resolver TenantResolver
	Store    Store
	Poster   CommentPoster
	Now      func() time.Time
}

var (
	ErrUnknownInstallation = errors.New("attribution: installation not registered")
	ErrNoMatchingApplyFix  = errors.New("attribution: no apply_fix row for repo+pr")
	ErrNilDependency       = errors.New("attribution: nil handler dependency")
)

func (h *Handler) OnMerged(ctx context.Context, ev MergedEvent) error {
	if h == nil || h.Resolver == nil || h.Store == nil || h.Poster == nil {
		return ErrNilDependency
	}
	if ev.PRNumber <= 0 || ev.RepoOwner == "" || ev.RepoName == "" {
		return fmt.Errorf("attribution: incomplete event: %+v", ev)
	}

	tenantID, err := h.Resolver.ResolveInstallation(ctx, ev.Provider, ev.InstallationID)
	if err != nil {
		return fmt.Errorf("attribution: resolve tenant: %w", err)
	}
	tctx := tenancy.Context{TenantID: tenantID}
	if err := tctx.Validate(); err != nil {
		return fmt.Errorf("attribution: bad tenant: %w", err)
	}

	repo := ev.RepoOwner + "/" + ev.RepoName
	row, err := h.Store.LookupByRepoPR(ctx, tctx, repo, ev.PRNumber)
	if err != nil {
		return fmt.Errorf("attribution: lookup: %w", err)
	}

	mergedAt := ev.MergedAt
	if mergedAt.IsZero() {
		mergedAt = h.now()
	}

	body := FormatComment(row, mergedAt)
	if err := h.Poster.Post(ctx, tctx, ev.RepoOwner, ev.RepoName, ev.PRNumber, body); err != nil {
		return fmt.Errorf("attribution: post: %w", err)
	}

	if err := h.Store.MarkMerged(ctx, tctx, row.ID, mergedAt); err != nil {
		return fmt.Errorf("attribution: mark merged: %w", err)
	}
	return nil
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

// FormatComment renders the post-merge follow-up. Deterministic so
// rerunning the handler against an idempotent CommentPoster produces
// byte-identical output (helps debug "did this fire twice?").
func FormatComment(r Row, mergedAt time.Time) string {
	var b strings.Builder
	b.WriteString("## Apply Fix merged\n\n")
	if r.MonthlySavingsUSDCents > 0 {
		fmt.Fprintf(&b, "Expected savings: **%s / month** · ~%s / year.\n\n",
			fmtUSD(r.MonthlySavingsUSDCents), fmtUSD(r.MonthlySavingsUSDCents*12))
	} else {
		b.WriteString("Expected savings: pending Receipt issuance.\n\n")
	}
	if r.Workload != "" {
		fmt.Fprintf(&b, "Workload: `%s`\n", r.Workload)
	}
	if r.Title != "" {
		fmt.Fprintf(&b, "Finding: %s\n", r.Title)
	}
	fmt.Fprintf(&b, "Merged at: %s\n\n", mergedAt.UTC().Format(time.RFC3339))
	b.WriteString("A signed Receipt comparing predicted vs realised savings against your AWS bill arrives 30 days from merge. ")
	b.WriteString("If post-merge metrics breach the 7-day baseline, the Auto-Rollback watchdog opens a revert PR.\n\n")
	b.WriteString("<sub>optiqor.dev — cost attribution follow-up</sub>\n")
	return b.String()
}

func fmtUSD(cents int64) string {
	if cents == 0 {
		return "$0"
	}
	d, c := cents/100, cents%100
	if c == 0 {
		return fmt.Sprintf("$%d", d)
	}
	return fmt.Sprintf("$%d.%02d", d, c)
}
