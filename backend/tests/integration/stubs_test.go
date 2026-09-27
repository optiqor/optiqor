//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/billing"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/vcs"
)

func TestStubs_BillingSources_ReturnErrNotImplemented(t *testing.T) {
	ctx := context.Background()
	tc := tenancy.Context{TenantID: "00000000-0000-0000-0000-000000000001"}
	win := billing.Window{
		Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	}

	for _, tc2 := range []struct {
		name   string
		source billing.Source
	}{
		{"aws-cur", billing.NewAWSCUR()},
		{"capacity", billing.NewCapacity()},
	} {
		t.Run(tc2.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked: %v", tc2.source.Name(), r)
				}
			}()
			_, err := tc2.source.Query(ctx, tc, win)
			if !errors.Is(err, billing.ErrNotImplemented) {
				t.Errorf("%s: err = %v, want ErrNotImplemented", tc2.source.Name(), err)
			}
		})
	}
}

func TestStubs_VCSGitHub_PostCommentAndOpenPR_ReturnErrNotImplemented(t *testing.T) {
	ctx := context.Background()
	gh := vcs.NewGitHub()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"PostComment", func() error {
			_, err := gh.PostComment(ctx, vcs.PullRequest{}, vcs.Comment{})
			return err
		}},
		{"OpenPR", func() error {
			_, err := gh.OpenPR(ctx, vcs.OpenPRRequest{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked: %v", tc.name, r)
				}
			}()
			if err := tc.call(); !errors.Is(err, vcs.ErrNotImplemented) {
				t.Errorf("%s: err = %v, want ErrNotImplemented", tc.name, err)
			}
		})
	}
}

func TestStubs_BillingRegistry_RegisterAndLookup(t *testing.T) {
	r := billing.Default()
	r.Register(billing.NewAWSCUR())
	r.Register(billing.NewCapacity())

	for _, name := range []string{"aws-cur", "capacity"} {
		t.Run(name, func(t *testing.T) {
			s, err := r.Lookup(name)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", name, err)
			}
			if s.Name() != name {
				t.Errorf("source.Name() = %q, want %q", s.Name(), name)
			}
		})
	}
}
