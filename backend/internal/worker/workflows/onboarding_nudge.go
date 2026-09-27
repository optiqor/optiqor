package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/optiqor/optiqor/internal/onboarding"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// NudgeChannel names where a nudge fires. The channel is the post-hoc
// audit field — "we paged CSM because the tenant sat at signed_up for
// 7 days" — so the value is part of the wire contract.
type NudgeChannel string

const (
	NudgeChannelEmail    NudgeChannel = "email"
	NudgeChannelInApp    NudgeChannel = "in_app"
	NudgeChannelCSMSlack NudgeChannel = "csm_slack"
)

// NudgeReason carries the funnel-position the nudge fires for. Mirrors
// onboarding.Stage so the operator dashboard can group by reason.
type NudgeReason string

const (
	NudgeReason24hNoVCS     NudgeReason = "24h_no_vcs"
	NudgeReason72hNoAgent   NudgeReason = "72h_no_agent"
	NudgeReason7dNoApplyFix NudgeReason = "7d_no_apply_fix"
)

// NudgeEvent is the payload the channel adapter (email gateway, Slack
// poster, etc.) renders. Kept minimal so the workflow stays decoupled
// from individual channel APIs.
type NudgeEvent struct {
	Tenant       tenancy.Context
	Reason       NudgeReason
	Channel      NudgeChannel
	Stage        onboarding.Stage
	StuckSince   time.Time
	Now          time.Time
	DashboardURL string
}

// NudgeNotifier delivers one event. Production wires per-channel
// implementations (Slack, email gateway, in-app event bus); tests use
// a recording fake.
type NudgeNotifier interface {
	Notify(ctx context.Context, ev NudgeEvent) error
}

// TenantOnboardingSource is the worker-side projection of the
// onboarding store. The workflow reads state at fire time — no caching
// — because the customer might have advanced between the cron tick
// firing and the workflow running.
type TenantOnboardingSource interface {
	Get(ctx context.Context, tenantID string) (onboarding.State, error)
}

// OnboardingNudgePayload is what the cron scheduler hands to each
// run. Tenant is the dispatch primitive (per-tenant queue). Reason
// determines the SLO window the workflow checks against.
type OnboardingNudgePayload struct {
	Reason       NudgeReason `json:"reason"`
	DashboardURL string      `json:"dashboard_url,omitempty"`
	Now          time.Time   `json:"now"`
}

// OnboardingNudge fires one nudge per tenant per cron tick when the
// tenant is stuck at the expected stage past the reason's SLO window.
// Idempotent on (tenant, reason, day) — the channel adapter
// de-duplicates the same day so a cron retry doesn't double-page.
type OnboardingNudge struct {
	Source   TenantOnboardingSource
	Notifier NudgeNotifier
}

func (OnboardingNudge) Name() string { return "onboarding_nudge" }

func (w OnboardingNudge) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	if w.Source == nil {
		return errors.New("onboarding_nudge: nil source")
	}
	if w.Notifier == nil {
		return errors.New("onboarding_nudge: nil notifier")
	}
	var p OnboardingNudgePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("onboarding_nudge: decode: %w", err)
	}
	if p.Now.IsZero() {
		p.Now = time.Now().UTC()
	}
	st, err := w.Source.Get(ctx, t.TenantID)
	if err != nil {
		return fmt.Errorf("onboarding_nudge: load state: %w", err)
	}

	ok, stage, since, channel := shouldFire(p.Reason, st, p.Now)
	if !ok {
		return nil
	}
	return w.Notifier.Notify(ctx, NudgeEvent{
		Tenant:       t,
		Reason:       p.Reason,
		Channel:      channel,
		Stage:        stage,
		StuckSince:   since,
		Now:          p.Now,
		DashboardURL: p.DashboardURL,
	})
}

// shouldFire owns the "is this tenant stuck" math. Pure function so
// the worker-side test pins every (reason × stage × elapsed) cell.
// Returns the stage the tenant is stuck on, when they arrived there,
// and which channel the nudge fires through.
func shouldFire(reason NudgeReason, st onboarding.State, now time.Time) (bool, onboarding.Stage, time.Time, NudgeChannel) {
	switch reason {
	case NudgeReason24hNoVCS:
		if st.Current != onboarding.StageSignedUp {
			return false, "", time.Time{}, ""
		}
		signup, ok := st.Reached[onboarding.StageSignedUp]
		if !ok {
			return false, "", time.Time{}, ""
		}
		if now.Sub(signup) < 24*time.Hour {
			return false, "", time.Time{}, ""
		}
		return true, onboarding.StageSignedUp, signup, NudgeChannelEmail
	case NudgeReason72hNoAgent:
		// Stuck at "I picked a repo / saw a PR analyzed but never installed
		// the agent" — surfacing in-app is the right channel because the
		// operator is the one who runs helm.
		if st.Current != onboarding.StageRepoSelected && st.Current != onboarding.StageFirstPRAnalyzed {
			return false, "", time.Time{}, ""
		}
		t, ok := st.Reached[st.Current]
		if !ok {
			return false, "", time.Time{}, ""
		}
		if now.Sub(t) < 72*time.Hour {
			return false, "", time.Time{}, ""
		}
		return true, st.Current, t, NudgeChannelInApp
	case NudgeReason7dNoApplyFix:
		// 7 days at agent_installed without a merged Apply Fix is the
		// canonical CSM trigger; tenant has the agent but the value loop
		// has not closed yet. Slack DM the CSM ($2k-cost intervention).
		if st.Current != onboarding.StageAgentInstalled {
			return false, "", time.Time{}, ""
		}
		t, ok := st.Reached[onboarding.StageAgentInstalled]
		if !ok {
			return false, "", time.Time{}, ""
		}
		if now.Sub(t) < 7*24*time.Hour {
			return false, "", time.Time{}, ""
		}
		return true, onboarding.StageAgentInstalled, t, NudgeChannelCSMSlack
	default:
		return false, "", time.Time{}, ""
	}
}
