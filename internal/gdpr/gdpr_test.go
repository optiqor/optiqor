package gdpr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lowplane/backend/internal/tenancy"
)

func TestRetentionPolicy(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		bucket string
		want   time.Duration
	}{
		{"prometheus", RetentionPrometheus},
		{"llm_calls", RetentionLLMCalls},
		{"receipts", RetentionReceipts},
		{"audit_log", RetentionAuditLog},
	}
	for _, tc := range cases {
		got, err := RetentionPolicy(tc.bucket, now)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.bucket, err)
			continue
		}
		want := now.Add(-tc.want)
		if !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", tc.bucket, got, want)
		}
	}
}

func TestRetentionPolicy_UnknownBucket(t *testing.T) {
	_, err := RetentionPolicy("nope", time.Now())
	var u *UnknownBucketError
	if !errors.As(err, &u) {
		t.Fatalf("got %v, want *UnknownBucketError", err)
	}
	if u.Bucket != "nope" {
		t.Errorf("bucket = %q", u.Bucket)
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

func TestNoopService_ExportValidatesTenant(t *testing.T) {
	if _, err := NoopService().Export(context.Background(), ExportRequest{}); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("expected ErrNoTenant; got %v", err)
	}
}

func TestNoopService_EraseValidatesTenant(t *testing.T) {
	if _, err := NoopService().Erase(context.Background(), EraseRequest{}); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("expected ErrNoTenant; got %v", err)
	}
}

func TestNoopService_ReturnsNotImplementedWithValidTenant(t *testing.T) {
	req := ExportRequest{Tenant: tenancy.Context{TenantID: "t1"}}
	if _, err := NoopService().Export(context.Background(), req); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented; got %v", err)
	}
	er := EraseRequest{Tenant: tenancy.Context{TenantID: "t1"}, Reason: "user request"}
	if _, err := NoopService().Erase(context.Background(), er); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented; got %v", err)
	}
}

// Sanity check that committed retention windows match the published
// numbers. Any change requires a coordinated update to docs/legal/.
func TestRetentionWindows_Committed(t *testing.T) {
	if RetentionPrometheus != 90*24*time.Hour {
		t.Errorf("RetentionPrometheus drift: %v", RetentionPrometheus)
	}
	if RetentionLLMCalls != 30*24*time.Hour {
		t.Errorf("RetentionLLMCalls drift: %v", RetentionLLMCalls)
	}
	if RetentionReceipts != 7*365*24*time.Hour {
		t.Errorf("RetentionReceipts drift: %v", RetentionReceipts)
	}
	if RetentionAuditLog != 7*365*24*time.Hour {
		t.Errorf("RetentionAuditLog drift: %v", RetentionAuditLog)
	}
	if ErasurePurgeWindow != 30*24*time.Hour {
		t.Errorf("ErasurePurgeWindow drift: %v", ErasurePurgeWindow)
	}
}
