package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func mustURI(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return u
}

func TestExtractTenant_ValidSPIFFEURI(t *testing.T) {
	r := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{URIs: []*url.URL{mustURI(t, "spiffe://optiqor.dev/tenant/11111111-1111-1111-1111-111111111111")}},
	}}}
	tc, err := ExtractTenant(r)
	if err != nil {
		t.Fatalf("ExtractTenant: %v", err)
	}
	if tc.TenantID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("tenant = %q", tc.TenantID)
	}
}

func TestExtractTenant_NoClientCert_ReturnsSentinel(t *testing.T) {
	r := &http.Request{}
	_, err := ExtractTenant(r)
	if !errors.Is(err, ErrNoClientCert) {
		t.Errorf("err = %v, want ErrNoClientCert", err)
	}
}

func TestExtractTenant_WrongTrustDomain_Rejected(t *testing.T) {
	r := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{URIs: []*url.URL{mustURI(t, "spiffe://attacker.example.com/tenant/abc")}},
	}}}
	_, err := ExtractTenant(r)
	if !errors.Is(err, ErrNoSPIFFEUSAN) && !errors.Is(err, ErrWrongTrust) {
		t.Errorf("err = %v", err)
	}
}

func TestExtractTenant_PathInjectionAttempt_Rejected(t *testing.T) {
	r := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{URIs: []*url.URL{mustURI(t, "spiffe://optiqor.dev/tenant/abc/extra")}},
	}}}
	_, err := ExtractTenant(r)
	if !errors.Is(err, ErrNoSPIFFEUSAN) && !errors.Is(err, ErrMalformedSPIFFE) {
		t.Errorf("err = %v", err)
	}
}

func TestExtractTenant_MixedURIs_PrefersValidSPIFFE(t *testing.T) {
	r := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{URIs: []*url.URL{
			mustURI(t, "https://www.optiqor.dev"),
			mustURI(t, "spiffe://optiqor.dev/tenant/22222222-2222-2222-2222-222222222222"),
		}},
	}}}
	tc, err := ExtractTenant(r)
	if err != nil {
		t.Fatalf("ExtractTenant: %v", err)
	}
	if tc.TenantID != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("tenant = %q", tc.TenantID)
	}
}
