package billing

import (
	"encoding/json"
	"errors"
	"io"
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

func TestSpikeHandler_Receive(t *testing.T) {
	happy := SpikeEnvelope{
		Tenant:           "t1",
		WorkloadID:       "wl-1",
		ObservedDeltaUSD: 120,
		ObservedAtUTC:    time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	}
	missingFields := SpikeEnvelope{Tenant: ""}
	dispatchOK := SpikeEnvelope{Tenant: "t1", WorkloadID: "wl-1"}

	for _, tc := range []struct {
		name         string
		method       string
		body         any
		dispatcher   *fakeSpikeDispatcher
		wantStatus   int
		wantDispatch int
	}{
		{
			name:         "happy path 202",
			method:       http.MethodPost,
			body:         happy,
			dispatcher:   &fakeSpikeDispatcher{},
			wantStatus:   http.StatusAccepted,
			wantDispatch: 1,
		},
		{
			name:       "rejects non-post",
			method:     http.MethodGet,
			body:       nil,
			dispatcher: &fakeSpikeDispatcher{},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "missing tenant and workload",
			method:     http.MethodPost,
			body:       missingFields,
			dispatcher: &fakeSpikeDispatcher{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "dispatch failure 502",
			method:     http.MethodPost,
			body:       dispatchOK,
			dispatcher: &fakeSpikeDispatcher{err: errors.New("downstream")},
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "unknown field rejected",
			method:     http.MethodPost,
			body:       map[string]any{"tenant": "t1", "workload_id": "wl-1", "bogus_typo_field": "x"},
			dispatcher: &fakeSpikeDispatcher{},
			wantStatus: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &SpikeHandler{Dispatcher: tc.dispatcher}
			var reqBody io.Reader = http.NoBody
			if tc.body != nil {
				b, err := json.Marshal(tc.body)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				reqBody = strings.NewReader(string(b))
			}
			req := httptest.NewRequest(tc.method, "/v1/cost-spikes", reqBody)
			w := httptest.NewRecorder()

			h.Receive(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if got := len(tc.dispatcher.got); got != tc.wantDispatch {
				t.Errorf("dispatched = %d, want %d", got, tc.wantDispatch)
			}
		})
	}
}
