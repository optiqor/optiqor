package gdpr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestRetentionPolicy(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		bucket string
		want   time.Duration // window subtracted from now; zero means UnknownBucketError
	}{
		{name: "prometheus", bucket: "prometheus", want: RetentionPrometheus},
		{name: "llm-calls", bucket: "llm_calls", want: RetentionLLMCalls},
		{name: "receipts", bucket: "receipts", want: RetentionReceipts},
		{name: "audit-log", bucket: "audit_log", want: RetentionAuditLog},
		{name: "unknown-bucket", bucket: "nope", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RetentionPolicy(tc.bucket, now)
			if tc.want == 0 {
				var u *UnknownBucketError
				if !errors.As(err, &u) {
					t.Fatalf("got %v, want *UnknownBucketError", err)
				}
				if u.Bucket != tc.bucket {
					t.Errorf("bucket = %q, want %q", u.Bucket, tc.bucket)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := now.Add(-tc.want)
			if !got.Equal(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestBuckets_Stable(t *testing.T) {
	got := Buckets()
	want := []string{"prometheus", "llm_calls", "receipts", "audit_log"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v vs %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Buckets()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestNoopService(t *testing.T) {
	ctx := context.Background()
	withTenant := tenancy.Context{TenantID: "t1"}

	for _, tc := range []struct {
		name    string
		call    func() error
		wantErr error
	}{
		{
			name: "export-missing-tenant",
			call: func() error {
				_, err := NoopService().Export(ctx, ExportRequest{})
				return err
			},
			wantErr: tenancy.ErrNoTenant,
		},
		{
			name: "erase-missing-tenant",
			call: func() error {
				_, err := NoopService().Erase(ctx, EraseRequest{})
				return err
			},
			wantErr: tenancy.ErrNoTenant,
		},
		{
			name: "export-with-tenant-not-implemented",
			call: func() error {
				_, err := NoopService().Export(ctx, ExportRequest{Tenant: withTenant})
				return err
			},
			wantErr: ErrNotImplemented,
		},
		{
			name: "erase-with-tenant-not-implemented",
			call: func() error {
				_, err := NoopService().Erase(ctx, EraseRequest{Tenant: withTenant, Reason: "user request"})
				return err
			},
			wantErr: ErrNotImplemented,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Drift on these constants requires a coordinated update to docs/legal/.
// They ship in the public DPA.
func TestRetentionWindows_Committed(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "prometheus-90d", got: RetentionPrometheus, want: 90 * 24 * time.Hour},
		{name: "llm-calls-30d", got: RetentionLLMCalls, want: 30 * 24 * time.Hour},
		{name: "receipts-7y", got: RetentionReceipts, want: 7 * 365 * 24 * time.Hour},
		{name: "audit-log-7y", got: RetentionAuditLog, want: 7 * 365 * 24 * time.Hour},
		{name: "erasure-purge-window-30d", got: ErasurePurgeWindow, want: 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}
