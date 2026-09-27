package attribution

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type fakeResolver struct {
	tenant string
	err    error
	calls  []struct {
		provider string
		id       int64
	}
}

func (f *fakeResolver) ResolveInstallation(_ context.Context, provider string, id int64) (string, error) {
	f.calls = append(f.calls, struct {
		provider string
		id       int64
	}{provider, id})
	if f.err != nil {
		return "", f.err
	}
	return f.tenant, nil
}

type fakeStore struct {
	mu        sync.Mutex
	row       Row
	lookupOK  bool
	lookupTC  tenancy.Context
	merged    []string
	mergedTC  tenancy.Context
	mergedAt  time.Time
	lookupErr error
	markErr   error
}

func (f *fakeStore) LookupByRepoPR(_ context.Context, t tenancy.Context, _ string, _ int) (Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookupTC = t
	if f.lookupErr != nil {
		return Row{}, f.lookupErr
	}
	if !f.lookupOK {
		return Row{}, ErrNoMatchingApplyFix
	}
	return f.row, nil
}

func (f *fakeStore) MarkMerged(_ context.Context, t tenancy.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged = append(f.merged, id)
	f.mergedTC = t
	f.mergedAt = at
	return f.markErr
}

type fakePoster struct {
	mu     sync.Mutex
	bodies []string
	tc     tenancy.Context
	err    error
}

func (p *fakePoster) Post(_ context.Context, t tenancy.Context, _, _ string, _ int, body string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bodies = append(p.bodies, body)
	p.tc = t
	return p.err
}

func TestOnMerged_HappyPath_PostsCommentAndMarksMerged(t *testing.T) {
	res := &fakeResolver{tenant: "11111111-1111-1111-1111-111111111111"}
	st := &fakeStore{
		lookupOK: true,
		row: Row{
			ID:                     "af-1",
			TenantID:               "11111111-1111-1111-1111-111111111111",
			PRURL:                  "https://github.com/acme/api/pull/42",
			Workload:               "api",
			Title:                  "CPU overprovisioned",
			MonthlySavingsUSDCents: 2920,
		},
	}
	post := &fakePoster{}
	h := &Handler{
		Resolver: res, Store: st, Poster: post,
		Now: func() time.Time { return time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC) },
	}

	mergedAt := time.Date(2026, 5, 27, 23, 12, 0, 0, time.UTC)
	err := h.OnMerged(context.Background(), MergedEvent{
		Provider: "github", InstallationID: 12345,
		RepoOwner: "acme", RepoName: "api", PRNumber: 42,
		MergedAt: mergedAt,
	})
	if err != nil {
		t.Fatalf("OnMerged: %v", err)
	}

	if len(res.calls) != 1 || res.calls[0].id != 12345 {
		t.Errorf("resolver calls = %+v", res.calls)
	}
	if len(post.bodies) != 1 {
		t.Fatalf("posts = %d, want 1", len(post.bodies))
	}
	got := post.bodies[0]
	for _, want := range []string{
		"Apply Fix merged",
		"$29.20 / month",
		"~$350.40 / year",
		"`api`",
		"CPU overprovisioned",
		"2026-05-27T23:12:00Z",
		"signed Receipt",
		"Auto-Rollback",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q\n%s", want, got)
		}
	}
	if len(st.merged) != 1 || st.merged[0] != "af-1" {
		t.Errorf("marked merged = %+v", st.merged)
	}
	if st.mergedAt != mergedAt {
		t.Errorf("merged_at = %v, want %v", st.mergedAt, mergedAt)
	}
	if post.tc.TenantID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("poster tenant = %q", post.tc.TenantID)
	}
}

func TestOnMerged_UnknownInstallation_BubblesUp(t *testing.T) {
	h := &Handler{
		Resolver: &fakeResolver{err: ErrUnknownInstallation},
		Store:    &fakeStore{},
		Poster:   &fakePoster{},
	}
	err := h.OnMerged(context.Background(), MergedEvent{
		Provider: "github", InstallationID: 999, RepoOwner: "a", RepoName: "b", PRNumber: 1,
	})
	if !errors.Is(err, ErrUnknownInstallation) {
		t.Fatalf("err = %v, want ErrUnknownInstallation", err)
	}
}

func TestOnMerged_NoApplyFixRow_DoesNotPost(t *testing.T) {
	res := &fakeResolver{tenant: "11111111-1111-1111-1111-111111111111"}
	st := &fakeStore{lookupOK: false}
	post := &fakePoster{}
	h := &Handler{Resolver: res, Store: st, Poster: post}

	err := h.OnMerged(context.Background(), MergedEvent{
		Provider: "github", InstallationID: 1, RepoOwner: "a", RepoName: "b", PRNumber: 1,
	})
	if !errors.Is(err, ErrNoMatchingApplyFix) {
		t.Fatalf("err = %v, want ErrNoMatchingApplyFix", err)
	}
	if len(post.bodies) != 0 {
		t.Errorf("must not post for a non-Optiqor PR")
	}
}

func TestOnMerged_NilDeps_FailsClosed(t *testing.T) {
	var h *Handler
	if err := h.OnMerged(context.Background(), MergedEvent{PRNumber: 1, RepoOwner: "a", RepoName: "b"}); !errors.Is(err, ErrNilDependency) {
		t.Errorf("nil handler: err = %v", err)
	}
	h = &Handler{Store: &fakeStore{}, Poster: &fakePoster{}}
	if err := h.OnMerged(context.Background(), MergedEvent{PRNumber: 1, RepoOwner: "a", RepoName: "b"}); !errors.Is(err, ErrNilDependency) {
		t.Errorf("missing resolver: err = %v", err)
	}
}

func TestParseGitHub_MergedEvent(t *testing.T) {
	body := []byte(`{
		"action": "closed",
		"pull_request": {"number": 7, "merged": true, "merged_at": "2026-05-27T23:12:00Z", "html_url": "https://github.com/acme/api/pull/7"},
		"repository": {"name": "api", "owner": {"login": "acme"}},
		"installation": {"id": 99}
	}`)
	ev, err := ParseGitHub(body)
	if err != nil {
		t.Fatalf("ParseGitHub: %v", err)
	}
	if ev.PRNumber != 7 || ev.RepoOwner != "acme" || ev.RepoName != "api" || ev.InstallationID != 99 {
		t.Errorf("event = %+v", ev)
	}
	if !ev.MergedAt.Equal(time.Date(2026, 5, 27, 23, 12, 0, 0, time.UTC)) {
		t.Errorf("merged_at = %v", ev.MergedAt)
	}
}

func TestParseGitHub_NotMerged_ReturnsSentinel(t *testing.T) {
	for _, raw := range []string{
		`{"action":"opened","pull_request":{"number":1,"merged":false},"repository":{"name":"a","owner":{"login":"o"}}}`,
		`{"action":"closed","pull_request":{"number":1,"merged":false},"repository":{"name":"a","owner":{"login":"o"}}}`,
	} {
		_, err := ParseGitHub([]byte(raw))
		if !errors.Is(err, ErrNotMerged) {
			t.Errorf("ParseGitHub(%q): err = %v, want ErrNotMerged", raw, err)
		}
	}
}

func TestFormatComment_NoSavings_StillUseful(t *testing.T) {
	row := Row{Workload: "worker", Title: "Container runs as root"}
	body := FormatComment(row, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(body, "pending Receipt") {
		t.Errorf("body missing pending-receipt fallback:\n%s", body)
	}
}
