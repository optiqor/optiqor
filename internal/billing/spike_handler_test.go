package billing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type fakeSpikeDispatcher struct {
	got []SpikeEnvelope
	err error
}

func (f *fakeSpikeDispatcher) DispatchSpike(_ tenancy.Context, e SpikeEnvelope) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, e)
	return nil
}

func TestSpikeHandler_HappyPath_202(t *testing.T) {
	d := &fakeSpikeDispatcher{}
	h := &SpikeHandler{Dispatcher: d}
	body, _ := json.Marshal(SpikeEnvelope{
		Tenant:           "t1",
		WorkloadID:       "wl-1",
		ObservedDeltaUSD: 120,
		ObservedAtUTC:    time.Now().UTC(),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/cost-spikes", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Receive(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
	if len(d.got) != 1 {
		t.Errorf("dispatched %d", len(d.got))
	}
}

func TestSpikeHandler_RejectsNonPost(t *testing.T) {
	h := &SpikeHandler{Dispatcher: &fakeSpikeDispatcher{}}
	req := httptest.NewRequest(http.MethodGet, "/v1/cost-spikes", http.NoBody)
	w := httptest.NewRecorder()
	h.Receive(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}

func TestSpikeHandler_RequiresTenantAndWorkload(t *testing.T) {
	h := &SpikeHandler{Dispatcher: &fakeSpikeDispatcher{}}
	body, _ := json.Marshal(SpikeEnvelope{Tenant: ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/cost-spikes", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Receive(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d", w.Code)
	}
}

func TestSpikeHandler_DispatchFailure_502(t *testing.T) {
	d := &fakeSpikeDispatcher{err: errors.New("downstream")}
	h := &SpikeHandler{Dispatcher: d}
	body, _ := json.Marshal(SpikeEnvelope{Tenant: "t1", WorkloadID: "wl-1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/cost-spikes", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Receive(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("code = %d", w.Code)
	}
}
