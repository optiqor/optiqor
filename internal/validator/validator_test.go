package validator

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestPipeline_Run(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-1"}

	for _, tc := range []struct {
		name     string
		cand     Candidate
		want     []string // verdict validator names in order
		rejected string   // first-hard-rejection reason substring; "" means accepted
	}{
		{
			name: "clean candidate passes all six",
			cand: Candidate{ProposedReplicas: 2, Signals: ClusterSignals{}},
		},
		{
			name: "pdb minAvailable hard-rejects",
			cand: Candidate{
				ProposedReplicas: 1,
				Signals:          ClusterSignals{PDB: &PDB{MinAvailable: 2}},
			},
			want:     []string{"pdb"},
			rejected: "minAvailable",
		},
		{
			name: "resourcequota hard-rejects when over",
			cand: Candidate{
				ProposedCPU: Quantity{Millicores: 2000},
				Signals:     ClusterSignals{Quota: &ResourceQuota{CPUMillicores: 4000, UsedCPUMilli: 3500}},
			},
			want:     []string{"resourcequota"},
			rejected: "ResourceQuota",
		},
		{
			name: "limitrange hard-rejects above max",
			cand: Candidate{
				ProposedCPU: Quantity{Millicores: 5000},
				Signals:     ClusterSignals{LimitRange: &LimitRange{MaxCPUMillicores: 2000}},
			},
			want:     []string{"limitrange"},
			rejected: "LimitRange max",
		},
		{
			name: "hpa in-bounds warns",
			cand: Candidate{
				ProposedReplicas: 5,
				Signals:          ClusterSignals{HPA: &HPA{MinReplicas: 2, MaxReplicas: 10}},
			},
			want: []string{"hpabounds"},
		},
		{
			name: "hpa under-min hard-rejects",
			cand: Candidate{
				ProposedReplicas: 1,
				Signals:          ClusterSignals{HPA: &HPA{MinReplicas: 3, MaxReplicas: 10}},
			},
			want:     []string{"hpabounds"},
			rejected: "HPA minReplicas",
		},
		{
			name: "many dependents hard-rejects",
			cand: Candidate{
				Signals: ClusterSignals{Dependents: []string{"a", "b", "c", "d"}},
			},
			want:     []string{"dependency"},
			rejected: "too many dependents",
		},
		{
			name: "oom recent + memory cut hard-rejects",
			cand: Candidate{
				ProposedMemory: Quantity{Bytes: 1 << 30},
				Signals:        ClusterSignals{OOMRecent: true},
			},
			want:     []string{"oom-recent"},
			rejected: "OOMKill",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPipeline(Default()...)
			res, err := p.Run(ctx, tnt, tc.cand)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.rejected == "" {
				if res.Rejected != nil {
					t.Errorf("expected accept, got rejection: %+v", res.Rejected)
				}
			} else {
				if res.Rejected == nil {
					t.Fatalf("expected rejection containing %q, got accept (verdicts=%+v)", tc.rejected, res.Verdicts)
				}
				if !strings.Contains(res.Rejected.Reason, tc.rejected) {
					t.Errorf("rejection reason %q missing %q", res.Rejected.Reason, tc.rejected)
				}
			}
			if len(tc.want) > 0 {
				got := make([]string, 0, len(res.Verdicts))
				for _, v := range res.Verdicts {
					got = append(got, v.Validator)
				}
				if !containsAll(got, tc.want) {
					t.Errorf("verdicts = %v, want all of %v", got, tc.want)
				}
			}
		})
	}
}

func TestNewPipeline_PanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on zero validators")
		}
	}()
	NewPipeline()
}

func containsAll(got, want []string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestPipeline_WithLogger_RecordsRejection(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	p := NewPipeline(PDBCheck{}).WithLogger(logger)
	_, _ = p.Run(context.Background(), tenancy.Context{TenantID: "tenant-log"}, Candidate{
		WorkloadID:       "api",
		DetectorID:       "memory-overprovisioned",
		ProposedReplicas: 1,
		Signals:          ClusterSignals{PDB: &PDB{MinAvailable: 2}},
	})
	out := buf.String()
	for _, want := range []string{"tenant-log", "pdb", "api", "memory-overprovisioned", "PDB minAvailable"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}

func TestPipeline_WithLogger_QuietOnAccept(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	p := NewPipeline(PDBCheck{}).WithLogger(logger)
	_, _ = p.Run(context.Background(), tenancy.Context{TenantID: "tenant-quiet"}, Candidate{
		WorkloadID:       "api",
		ProposedReplicas: 5,
		Signals:          ClusterSignals{PDB: &PDB{MinAvailable: 2}},
	})
	if buf.Len() != 0 {
		t.Errorf("logger should be silent on accept; got: %s", buf.String())
	}
}
