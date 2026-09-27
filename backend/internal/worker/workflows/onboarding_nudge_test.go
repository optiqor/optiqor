package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/onboarding"
	"github.com/optiqor/optiqor/internal/tenancy"
)

type stubOnboardingSource struct {
	state onboarding.State
	err   error
}

func (s stubOnboardingSource) Get(_ context.Context, _ string) (onboarding.State, error) {
	return s.state, s.err
}

type recordingNudgeNotifier struct {
	mu     sync.Mutex
	events []NudgeEvent
	err    error
}

func (r *recordingNudgeNotifier) Notify(_ context.Context, ev NudgeEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return r.err
}

func mkReached(reached map[onboarding.Stage]time.Time) onboarding.State {
	cur := onboarding.StageSignedUp
	for _, s := range onboarding.Stages {
		if _, ok := reached[s]; ok {
			cur = s
		}
	}
	return onboarding.State{Current: cur, Reached: reached}
}

func TestOnboardingNudge_24hNoVCS_Fires(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	signup := now.Add(-25 * time.Hour)
	src := stubOnboardingSource{state: mkReached(map[onboarding.Stage]time.Time{onboarding.StageSignedUp: signup})}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason24hNoVCS, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(notif.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(notif.events))
	}
	ev := notif.events[0]
	if ev.Channel != NudgeChannelEmail {
		t.Errorf("channel = %s, want email", ev.Channel)
	}
	if ev.Stage != onboarding.StageSignedUp {
		t.Errorf("stage = %s, want signed_up", ev.Stage)
	}
}

func TestOnboardingNudge_24hNoVCS_TooEarlyNoops(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	signup := now.Add(-12 * time.Hour)
	src := stubOnboardingSource{state: mkReached(map[onboarding.Stage]time.Time{onboarding.StageSignedUp: signup})}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason24hNoVCS, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatal(err)
	}
	if len(notif.events) != 0 {
		t.Errorf("too-early should not fire, got %v", notif.events)
	}
}

func TestOnboardingNudge_24h_DoesNotFireWhenAdvancedPastSignedUp(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	signup := now.Add(-72 * time.Hour)
	st := mkReached(map[onboarding.Stage]time.Time{
		onboarding.StageSignedUp:     signup,
		onboarding.StageVCSConnected: signup.Add(time.Hour),
	})
	src := stubOnboardingSource{state: st}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason24hNoVCS, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatal(err)
	}
	if len(notif.events) != 0 {
		t.Errorf("advanced tenant must not get the 24h nudge, got %v", notif.events)
	}
}

func TestOnboardingNudge_72hNoAgent_FiresFromRepoSelected(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stuck := now.Add(-80 * time.Hour)
	st := mkReached(map[onboarding.Stage]time.Time{
		onboarding.StageSignedUp:     stuck.Add(-time.Hour),
		onboarding.StageVCSConnected: stuck.Add(-30 * time.Minute),
		onboarding.StageRepoSelected: stuck,
	})
	src := stubOnboardingSource{state: st}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason72hNoAgent, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatal(err)
	}
	if len(notif.events) != 1 || notif.events[0].Channel != NudgeChannelInApp {
		t.Errorf("expect one in_app nudge, got %+v", notif.events)
	}
}

func TestOnboardingNudge_7dNoApplyFix_FiresCSM(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	installed := now.Add(-9 * 24 * time.Hour)
	st := mkReached(map[onboarding.Stage]time.Time{
		onboarding.StageSignedUp:       installed.Add(-2 * time.Hour),
		onboarding.StageAgentInstalled: installed,
	})
	src := stubOnboardingSource{state: st}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason7dNoApplyFix, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatal(err)
	}
	if len(notif.events) != 1 || notif.events[0].Channel != NudgeChannelCSMSlack {
		t.Errorf("expect one csm_slack nudge, got %+v", notif.events)
	}
}

func TestOnboardingNudge_NoopAfterApplyFixMerged(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	installed := now.Add(-10 * 24 * time.Hour)
	st := mkReached(map[onboarding.Stage]time.Time{
		onboarding.StageSignedUp:       installed.Add(-2 * time.Hour),
		onboarding.StageAgentInstalled: installed,
		onboarding.StageFirstApplyFix:  now.Add(-2 * time.Hour),
	})
	src := stubOnboardingSource{state: st}
	notif := &recordingNudgeNotifier{}
	wf := OnboardingNudge{Source: src, Notifier: notif}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason7dNoApplyFix, Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatal(err)
	}
	if len(notif.events) != 0 {
		t.Errorf("tenant past the gate must not be nudged, got %v", notif.events)
	}
}

func TestOnboardingNudge_NilDepsRejected(t *testing.T) {
	wf := OnboardingNudge{}
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, []byte(`{"reason":"24h_no_vcs"}`))
	if err == nil {
		t.Error("nil source must error")
	}
	wf.Source = stubOnboardingSource{}
	err = wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, []byte(`{"reason":"24h_no_vcs"}`))
	if err == nil {
		t.Error("nil notifier must error")
	}
}

func TestOnboardingNudge_SourceErrorPropagates(t *testing.T) {
	wf := OnboardingNudge{
		Source:   stubOnboardingSource{err: errors.New("db down")},
		Notifier: &recordingNudgeNotifier{},
	}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: NudgeReason24hNoVCS, Now: time.Now()})
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw)
	if err == nil {
		t.Error("source error must propagate to caller (Temporal retries)")
	}
}

func TestOnboardingNudge_UnknownReasonNoops(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	st := mkReached(map[onboarding.Stage]time.Time{onboarding.StageSignedUp: now.Add(-72 * time.Hour)})
	wf := OnboardingNudge{Source: stubOnboardingSource{state: st}, Notifier: &recordingNudgeNotifier{}}
	raw, _ := json.Marshal(OnboardingNudgePayload{Reason: "unknown", Now: now})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
