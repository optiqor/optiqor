package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/optiqor/backend/internal/worker"
)

func TestRegisterWorkflows_BindsAllFive(t *testing.T) {
	d := worker.NewInMemory()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := registerWorkflows(d, log); err != nil {
		t.Fatalf("register: %v", err)
	}
	want := []string{"apply_fix", "cost_spike", "echo", "receipt_issue", "rollback_watchdog"}
	got := d.Workflows()
	if len(got) != len(want) {
		t.Fatalf("workflows = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("workflows[%d] = %q, want %q", i, got[i], w)
		}
	}
}
