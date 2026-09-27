package receipts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func mkValidReceipt() Receipt {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	return Receipt{
		ID:                       "rcpt_1",
		TenantID:                 "t1",
		Workload:                 "api",
		ApplyFixID:               "afix_1",
		ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 1000,
		RealisedSavingsUSDCents:  950,
		CloudBillSource:          "aws/cur:2026-05",
		IssuedAtUTC:              now,
	}
}

func TestInMemoryStore_RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seed    bool
		wantID  string
		wantErr error
	}{
		{name: "save then get round-trips", seed: true, wantID: "rcpt_1"},
		{name: "missing id returns sentinel", seed: false, wantErr: ErrReceiptNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewInMemoryStore()
			lookupID := "rcpt_missing"
			if tc.seed {
				if err := s.Save(context.Background(), tenancy.Context{TenantID: "t1"}, "rcpt_1", "abc", mkValidReceipt()); err != nil {
					t.Fatalf("Save: %v", err)
				}
				lookupID = "rcpt_1"
			}
			signed, r, err := s.Get(context.Background(), lookupID)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if signed != "abc" {
				t.Errorf("signed = %q", signed)
			}
			if r.ID != tc.wantID {
				t.Errorf("receipt id = %q", r.ID)
			}
		})
	}
}

// setupHandler issues a signed receipt for "rcpt_1" under key "k1" and
// returns a Handler whose registry trusts that key. Tests covering the
// "verified" path share this; 404 cases skip it via seed=false.
func setupHandler(t *testing.T, seed bool) *Handler {
	t.Helper()
	store := NewInMemoryStore()
	reg := NewStaticRegistry()
	if seed {
		iss, pub, err := GenerateIssuer("k1")
		if err != nil {
			t.Fatalf("GenerateIssuer: %v", err)
		}
		signed, err := iss.Sign(mkValidReceipt())
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		r := mkValidReceipt()
		r.IssuerKeyID = "k1"
		if err := store.Save(context.Background(), tenancy.Context{TenantID: "t1"}, "rcpt_1", signed, r); err != nil {
			t.Fatalf("Save: %v", err)
		}
		reg.Add("k1", pub)
	}
	return &Handler{Store: store, Registry: reg}
}

func TestHandler_Routes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seed     bool
		method   string
		path     string
		wantCode int
		check    func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:     "get returns verified json for known id",
			seed:     true,
			method:   http.MethodGet,
			path:     "/v1/receipts/rcpt_1",
			wantCode: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				t.Helper()
				var resp VerifyResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !resp.Verified {
					t.Errorf("verified = false; want true")
				}
				if resp.IssuerKeyID != "k1" {
					t.Errorf("issuer key id = %q", resp.IssuerKeyID)
				}
			},
		},
		{
			name:     "get unknown id returns 404",
			seed:     false,
			method:   http.MethodGet,
			path:     "/v1/receipts/missing",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "verify page renders html with signature status",
			seed:     true,
			method:   http.MethodGet,
			path:     "/v/rcpt_1",
			wantCode: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				t.Helper()
				if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
					t.Errorf("content-type = %q", ct)
				}
				body := w.Body.String()
				for _, want := range []string{
					"<!doctype html>",
					"Verified Receipt",
					"rcpt_1",
					"signature verified",
				} {
					if !strings.Contains(body, want) {
						t.Errorf("verifier missing %q", want)
					}
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := setupHandler(t, tc.seed)
			mux := http.NewServeMux()
			h.Mount(mux)
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
			}
			if tc.check != nil {
				tc.check(t, w)
			}
		})
	}
}

func TestHandler_Get_RejectsNonGet(t *testing.T) {
	// Mount-level method routing already filters POST, but Get is also
	// exposed directly via the receiver — preserve its own 405 guard.
	h := &Handler{Store: NewInMemoryStore(), Registry: NewStaticRegistry()}
	req := httptest.NewRequest(http.MethodPost, "/v1/receipts/x", http.NoBody)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}
