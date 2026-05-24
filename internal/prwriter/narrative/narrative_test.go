package narrative

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestTemplateGenerator_Generate(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-1"}

	for _, tc := range []struct {
		name    string
		req     Request
		want    []string // substrings the output MUST contain
		wantErr bool
	}{
		{
			name: "cpu cut with p95 + savings",
			req: Request{
				Workload: "api", BeforeCPUMilli: 2000, AfterCPUMilli: 1500,
				P95CPUMilli: 1200, Confidence: "high", MonthlyUSDCents: 34000,
			},
			want: []string{
				"trims api's CPU request",
				"2 vCPU",
				"1.5 vCPU",
				"P95",
				"high",
				"$340/mo",
			},
		},
		{
			name: "memory cut formatted Gi",
			req: Request{
				Workload: "cache", BeforeMemoryBytes: 8 << 30, AfterMemoryBytes: 4 << 30,
				Confidence: "medium",
			},
			want: []string{
				"trims cache's memory",
				"8Gi", "4Gi",
				"medium",
			},
		},
		{
			name: "no cut but workload supplied",
			req: Request{
				Workload: "worker", Confidence: "low",
			},
			want: []string{"configuration change on worker", "low"},
		},
		{
			name:    "empty workload errors",
			req:     Request{},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TemplateGenerator{}.Generate(ctx, tnt, tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("narrative %q missing %q", got, w)
				}
			}
			if !strings.HasSuffix(got, ".") {
				t.Errorf("narrative %q must end with a period", got)
			}
		})
	}
}

func TestFormatCPU(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{2000, "2 vCPU"},
		{1500, "1.5 vCPU"},
		{500, "500m"},
		{100, "100m"},
	} {
		if got := formatCPU(tc.in); got != tc.want {
			t.Errorf("formatCPU(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
