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

	"github.com/optiqor/backend/internal/tenancy"
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

func TestInMemoryStore_SaveGet(t *testing.T) {
	s := NewInMemoryStore()
	if err := s.Save(context.Background(), tenancy.Context{TenantID: "t1"}, "rcpt_1", "abc", mkValidReceipt()); err != nil {
		t.Fatal(err)
	}
	signed, r, err := s.Get(context.Background(), "rcpt_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if signed != "abc" {
		t.Errorf("signed = %q", signed)
	}
	if r.ID != "rcpt_1" {
		t.Errorf("receipt id = %q", r.ID)
	}
}

func TestInMemoryStore_Unknown_ReturnsNotFound(t *testing.T) {
	s := NewInMemoryStore()
	if _, _, err := s.Get(context.Background(), "rcpt_missing"); !errors.Is(err, ErrReceiptNotFound) {
		t.Errorf("want ErrReceiptNotFound, got %v", err)
	}
}

func TestHandler_GetVerified_True(t *testing.T) {
	iss, pub, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	store := NewInMemoryStore()
	signed, err := iss.Sign(mkValidReceipt())
	if err != nil {
		t.Fatal(err)
	}
	r := mkValidReceipt()
	r.IssuerKeyID = "k1"
	_ = store.Save(context.Background(), tenancy.Context{TenantID: "t1"}, "rcpt_1", signed, r)

	reg := NewStaticRegistry()
	reg.Add("k1", pub)
	h := &Handler{Store: store, Registry: reg}

	req := httptest.NewRequest(http.MethodGet, "/v1/receipts/rcpt_1", http.NoBody)
	mux := http.NewServeMux()
	h.Mount(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	var resp VerifyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Verified {
		t.Errorf("verified = false; want true")
	}
	if resp.IssuerKeyID != "k1" {
		t.Errorf("issuer key id = %q", resp.IssuerKeyID)
	}
}

func TestHandler_GetUnknown_404(t *testing.T) {
	h := &Handler{Store: NewInMemoryStore(), Registry: NewStaticRegistry()}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/receipts/missing", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d", w.Code)
	}
}

func TestHandler_VerifyPage_HTMLAndStatus(t *testing.T) {
	iss, pub, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	store := NewInMemoryStore()
	signed, _ := iss.Sign(mkValidReceipt())
	r := mkValidReceipt()
	r.IssuerKeyID = "k1"
	_ = store.Save(context.Background(), tenancy.Context{TenantID: "t1"}, "rcpt_1", signed, r)
	reg := NewStaticRegistry()
	reg.Add("k1", pub)
	h := &Handler{Store: store, Registry: reg}

	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v/rcpt_1", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
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
}

func TestHandler_RejectsNonGet(t *testing.T) {
	h := &Handler{Store: NewInMemoryStore(), Registry: NewStaticRegistry()}
	req := httptest.NewRequest(http.MethodPost, "/v1/receipts/x", http.NoBody)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}
