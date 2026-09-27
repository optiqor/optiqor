package attribution

import (
	"context"
	"fmt"

	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/vcs"
)

// VCSPoster adapts a vcs.Source to CommentPoster. Production wires the
// real github source; vcs.GitHub.PostComment owns the actual API call
// so retries + rate limiting compose in one place.
type VCSPoster struct {
	Source vcs.Source
}

func NewVCSPoster(s vcs.Source) *VCSPoster { return &VCSPoster{Source: s} }

var _ CommentPoster = (*VCSPoster)(nil)

func (p *VCSPoster) Post(ctx context.Context, _ tenancy.Context, owner, name string, prNumber int, body string) error {
	pr := vcs.PullRequest{
		Repo:   vcs.Repo{Provider: p.Source.Provider(), Owner: owner, Name: name},
		Number: prNumber,
	}
	if _, err := p.Source.PostComment(ctx, pr, vcs.Comment{Body: body}); err != nil {
		return fmt.Errorf("attribution/poster: %w", err)
	}
	return nil
}
