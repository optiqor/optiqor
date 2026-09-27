package validator

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/platform/telemetry"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestMetrics_RecordsKnownAndUnknownReasons(t *testing.T) {
	reg := telemetry.NewRegistry()
	m := NewMetrics(reg)

	m.RecordReject("pdb")
	m.RecordReject("pdb")
	m.RecordReject("oom-recent")
	m.RecordReject("unknown-validator")

	if got := m.counters["pdb"].Value(); got != 2 {
		t.Errorf("pdb counter = %v, want 2", got)
	}
	if got := m.counters["oom-recent"].Value(); got != 1 {
		t.Errorf("oom-recent counter = %v, want 1", got)
	}
	if got := m.any.Value(); got != 1 {
		t.Errorf("other counter = %v, want 1", got)
	}
}

func TestPipeline_RecordsRejectMetric(t *testing.T) {
	reg := telemetry.NewRegistry()
	m := NewMetrics(reg)
	p := NewPipeline(Default()...).WithMetrics(m)

	_, err := p.Run(context.Background(), tenancy.Context{TenantID: "t-1"}, Candidate{
		ProposedReplicas: 1,
		Signals:          ClusterSignals{PDB: &PDB{MinAvailable: 2}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m.counters["pdb"].Value() != 1 {
		t.Errorf("pdb counter = %v, want 1", m.counters["pdb"].Value())
	}
}
