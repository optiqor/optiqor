package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/parser"
	"github.com/optiqor/optiqor/internal/prwriter"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// PRPublisher is the seam to the GitHub API. Production wires
// google/go-github; tests use a fake.
type PRPublisher interface {
	Publish(ctx context.Context, t tenancy.Context, p PullRequest) (PRResult, error)
}

type PullRequest struct {
	RepoOwner   string
	RepoName    string
	HeadBranch  string
	BaseBranch  string
	Title       string
	Body        string
	UnifiedDiff string
	ApplyFixID  string
}

type PRResult struct {
	URL    string
	Number int
}

type ApplyFixPayload struct {
	RepoOwner  string        `json:"repo_owner"`
	RepoName   string        `json:"repo_name"`
	BaseBranch string        `json:"base_branch"`
	ChartPath  string        `json:"chart_path"`
	ChartYAML  string        `json:"chart_yaml"`
	Model      string        `json:"model"`
	Finding    rules.Finding `json:"finding"`
	ApplyFixID string        `json:"apply_fix_id"`
	Now        time.Time     `json:"now"`
}

// ApplyFix runs the deterministic CLI rule engine, the LLM diff
// generator, and the prwriter renderer, then hands a PR payload to the
// GitHub layer. One per process, registered with worker.Dispatcher.
type ApplyFix struct {
	Composer  *agent.Composer
	Publisher PRPublisher
}

func (ApplyFix) Name() string { return "apply_fix" }

func (w ApplyFix) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	var p ApplyFixPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("apply_fix: decode: %w", err)
	}
	if w.Composer == nil {
		return fmt.Errorf("apply_fix: nil composer")
	}
	if w.Publisher == nil {
		return fmt.Errorf("apply_fix: nil publisher")
	}

	workloads, err := parser.ParseValues(stringReader(p.ChartYAML))
	if err != nil {
		return fmt.Errorf("apply_fix: parse chart: %w", err)
	}
	primary := primaryWorkload(workloads, p.Finding.Workload)

	resp, err := w.Composer.GenerateFix(ctx, t, agent.FixRequest{
		Finding:   p.Finding,
		ChartYAML: p.ChartYAML,
		Workload:  primary,
		Model:     p.Model,
	})
	if err != nil {
		return fmt.Errorf("apply_fix: compose: %w", err)
	}

	body, err := prwriter.Render(prwriter.Comment{
		Chart:                  fmt.Sprintf("%s/%s/%s", p.RepoOwner, p.RepoName, p.ChartPath),
		Tenant:                 t.TenantID,
		Workloads:              1,
		Findings:               []rules.Finding{p.Finding},
		MonthlySavingsUSDCents: p.Finding.MonthlyUSDCents,
		AnnualSavingsUSDCents:  p.Finding.MonthlyUSDCents * 12,
		Mode:                   prwriter.ModeAgent,
		GeneratedAt:            p.Now,
		SecurityVisible:        p.Finding.Category == rules.CategorySecurity,
	})
	if err != nil {
		return fmt.Errorf("apply_fix: render: %w", err)
	}

	_, err = w.Publisher.Publish(ctx, t, PullRequest{
		RepoOwner:   p.RepoOwner,
		RepoName:    p.RepoName,
		HeadBranch:  fmt.Sprintf("optiqor/apply-fix/%s", p.ApplyFixID),
		BaseBranch:  p.BaseBranch,
		Title:       fmt.Sprintf("optiqor: %s — %s", p.Finding.Workload, p.Finding.Title),
		Body:        body + "\n\n---\n" + resp.Explanation,
		UnifiedDiff: resp.UnifiedDiff,
		ApplyFixID:  p.ApplyFixID,
	})
	if err != nil {
		return fmt.Errorf("apply_fix: publish: %w", err)
	}
	return nil
}

func primaryWorkload(ws []parser.Workload, want string) string {
	for _, w := range ws {
		if w.Name == want {
			return w.Name
		}
	}
	if len(ws) > 0 {
		return ws[0].Name
	}
	return want
}
