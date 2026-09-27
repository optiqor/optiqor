package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestHash_IsDeterministicAndStable(t *testing.T) {
	const sample = "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n"
	a := Hash(sample)
	b := Hash(sample)
	if a != b {
		t.Errorf("Hash non-deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hash len = %d, want 64", len(a))
	}
}

func TestInMemoryAuditor_Record(t *testing.T) {
	ctx := context.Background()
	good := tenancy.Context{TenantID: "t-1"}

	for _, tc := range []struct {
		name    string
		tenant  tenancy.Context
		record  Record
		wantErr string
	}{
		{
			name:   "happy path stores",
			tenant: good,
			record: Record{ID: "a", InputSHA256: Hash("in"), OutputSHA256: Hash("out"), Model: "sonnet", At: time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)},
		},
		{
			name:    "empty tenant rejected",
			tenant:  tenancy.Context{},
			record:  Record{InputSHA256: Hash("in"), OutputSHA256: Hash("out")},
			wantErr: "tenant",
		},
		{
			name:    "missing input hash rejected",
			tenant:  good,
			record:  Record{OutputSHA256: Hash("out")},
			wantErr: "missing",
		},
		{
			name:    "missing output hash rejected",
			tenant:  good,
			record:  Record{InputSHA256: Hash("in")},
			wantErr: "missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewInMemoryAuditor()
			err := a.Record(ctx, tc.tenant, tc.record)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				if a.Len() != 0 {
					t.Errorf("Len = %d, want 0 on error", a.Len())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if a.Len() != 1 {
				t.Errorf("Len = %d, want 1", a.Len())
			}
		})
	}
}

func TestNullAuditor_AcceptsEverything(t *testing.T) {
	if err := (NullAuditor{}).Record(context.Background(), tenancy.Context{}, Record{}); err != nil {
		t.Errorf("NullAuditor returned err: %v", err)
	}
}
