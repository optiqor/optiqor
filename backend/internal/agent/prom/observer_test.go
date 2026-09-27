package prom

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/platform/telemetry"
)

func TestTelemetryObserver_RecordsLatencyAndErrors(t *testing.T) {
	reg := telemetry.NewRegistry()
	obs := NewTelemetryObserver(reg)

	obs.ObserveQuery("query", "ok", 250*time.Millisecond)
	obs.ObserveQuery("query_range", "ok", 3*time.Second)
	obs.ObserveQuery("query", "401", 50*time.Millisecond)
	obs.ObserveQuery("query_range", "5xx", 1500*time.Millisecond)

	var buf bytes.Buffer
	if err := reg.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`optiqor_agent_prom_query_seconds`,
		`kind="query"`,
		`kind="query_range"`,
		`optiqor_agent_prom_errors_total`,
		`status="401"`,
		`status="5xx"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected metric output to include %q\n%s", want, out)
		}
	}
}

func TestTelemetryObserver_NilRegistryReturnsNil(t *testing.T) {
	if NewTelemetryObserver(nil) != nil {
		t.Error("nil registry should return nil observer")
	}
}

func TestTelemetryObserver_NilSafeOnObserve(t *testing.T) {
	var obs *TelemetryObserver
	obs.ObserveQuery("query", "ok", time.Millisecond)
}

func TestTelemetryObserver_OkStatusDoesNotIncrementErrors(t *testing.T) {
	reg := telemetry.NewRegistry()
	obs := NewTelemetryObserver(reg)
	for i := 0; i < 5; i++ {
		obs.ObserveQuery("query", "ok", time.Millisecond)
	}
	var buf bytes.Buffer
	if err := reg.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if strings.Contains(buf.String(), "optiqor_agent_prom_errors_total") {
		t.Errorf("ok status created an errors counter:\n%s", buf.String())
	}
}
