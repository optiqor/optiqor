package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/applyfix/latency"
	"github.com/optiqor/optiqor/internal/operators"
	"github.com/optiqor/optiqor/internal/parser"
	"github.com/optiqor/optiqor/internal/prwriter"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/validator"
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

	// Agent-attached signals; populated when the dispatcher runs from
	// in-cluster data. Empty on webhook-driven paths (sandbox + Phase-1
	// dev). Nil-safe downstream: validators no-op on missing signals.
	WorkloadOwners []operators.OwnerRef     `json:"workload_owners,omitempty"`
	ClusterSignals validator.ClusterSignals `json:"cluster_signals,omitempty"`

	// Quantities the cost engine proposes, used by the validator to
	// gate the candidate against ClusterSignals. All optional.
	ProposedCPUMilli int64 `json:"proposed_cpu_milli,omitempty"`
	ProposedMemoryB  int64 `json:"proposed_memory_b,omitempty"`
	ProposedReplicas int   `json:"proposed_replicas,omitempty"`
}

type ApplyFix struct {
	Composer  *agent.Composer
	Gate      *gate.Pipeline
	Validator *validator.Pipeline // optional; nil skips Validation-Before-Recommendation
	Publisher PRPublisher
	Latency   *latency.Recorder // optional; nil disables histogram recording

	// SkepticMode forces the strictest possible safety floor. Maps to:
	// validator becomes mandatory (returns an error if not configured),
	// gate must reach Passed (NotImplemented stages are rejected), and
	// any non-empty warn-level verdict is treated as a hard rejection.
	// Use for new tenants + new clusters until the analysis surface
	// has earned trust.
	SkepticMode bool

	// OwnerResolve is the function the operator detector uses to walk
	// the owner-chain. Production wires it to the agent's informer
	// cache; nil disables operator gating (webhook-driven paths).
	OwnerResolve func(operators.OwnerRef) (operators.OwnerRef, bool)
}

func (ApplyFix) Name() string { return "apply_fix" }

func (w ApplyFix) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	totalStart := time.Now()
	defer func() {
		w.Latency.Observe(latency.StepTotal, time.Since(totalStart))
	}()

	var p ApplyFixPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("apply_fix: decode: %w", err)
	}
	if w.Composer == nil {
		return fmt.Errorf("apply_fix: nil composer")
	}
	if w.Gate == nil {
		return fmt.Errorf("apply_fix: nil gate")
	}
	if w.Publisher == nil {
		return fmt.Errorf("apply_fix: nil publisher")
	}
	if w.SkepticMode && w.Validator == nil {
		return fmt.Errorf("apply_fix: skeptic mode requires a Validator")
	}

	parseStart := time.Now()
	workloads, err := parser.ParseValues(stringReader(p.ChartYAML))
	w.Latency.Observe(latency.StepParse, time.Since(parseStart))
	if err != nil {
		return fmt.Errorf("apply_fix: parse chart: %w", err)
	}
	primary := primaryWorkload(workloads, p.Finding.Workload)

	if w.OwnerResolve != nil && len(p.WorkloadOwners) > 0 {
		gateStart := time.Now()
		cls := operators.Classify(operators.Workload{
			Namespace: "",
			Kind:      "Deployment",
			Name:      primary,
			Owners:    p.WorkloadOwners,
		}, w.OwnerResolve)
		w.Latency.Observe(latency.StepOperatorGate, time.Since(gateStart))
		if !cls.Direct {
			return fmt.Errorf("apply_fix: operator-owned workload %s rejected (%s)", primary, cls.String())
		}
	}

	composeStart := time.Now()
	resp, err := w.Composer.GenerateFix(ctx, t, agent.FixRequest{
		Finding:   p.Finding,
		ChartYAML: p.ChartYAML,
		Workload:  primary,
		Model:     p.Model,
	})
	w.Latency.Observe(latency.StepCompose, time.Since(composeStart))
	if err != nil {
		return fmt.Errorf("apply_fix: compose: %w", err)
	}

	gateStart := time.Now()
	gateRes, err := w.Gate.Run(ctx, t, gate.Candidate{
		ApplyFixID:  p.ApplyFixID,
		ChartYAML:   p.ChartYAML,
		UnifiedDiff: resp.UnifiedDiff,
		Workload:    primary,
	})
	w.Latency.Observe(latency.StepGate, time.Since(gateStart))
	if err != nil {
		return fmt.Errorf("apply_fix: gate: %w", err)
	}
	if w.SkepticMode {
		for _, sr := range gateRes.Stages {
			if sr.Status != gate.StatusPassed {
				return fmt.Errorf("apply_fix: skeptic mode rejects stage %s %s", sr.Stage, sr.Status)
			}
		}
	}

	if w.Validator != nil {
		valStart := time.Now()
		res, err := w.Validator.Run(ctx, t, validator.Candidate{
			WorkloadID:       primary,
			DetectorID:       p.Finding.DetectorID,
			Title:            p.Finding.Title,
			ProposedCPU:      validator.Quantity{Millicores: p.ProposedCPUMilli},
			ProposedMemory:   validator.Quantity{Bytes: p.ProposedMemoryB},
			ProposedReplicas: p.ProposedReplicas,
			MonthlyUSDCents:  p.Finding.MonthlyUSDCents,
			Signals:          p.ClusterSignals,
		})
		w.Latency.Observe(latency.StepValidator, time.Since(valStart))
		if err != nil {
			return fmt.Errorf("apply_fix: validator: %w", err)
		}
		if res.Rejected != nil {
			return fmt.Errorf("apply_fix: validator rejected: %s — %s", res.Rejected.Validator, res.Rejected.Reason)
		}
		if w.SkepticMode {
			for _, v := range res.Verdicts {
				if v.Severity == validator.SeverityWarn {
					return fmt.Errorf("apply_fix: skeptic mode rejects warn-level verdict from %s: %s", v.Validator, v.Reason)
				}
			}
		}
	}

	renderStart := time.Now()
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
	w.Latency.Observe(latency.StepRender, time.Since(renderStart))
	if err != nil {
		return fmt.Errorf("apply_fix: render: %w", err)
	}

	publishStart := time.Now()
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
	w.Latency.Observe(latency.StepPublish, time.Since(publishStart))
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
