// Package mtls extracts tenant identity from an mTLS client cert.
// The agent presents a SPIFFE-compatible SVID; the URI SAN encodes
// the tenant id as spiffe://optiqor.dev/tenant/<uuid>. The api's
// middleware swaps for HeaderTenantExtractor when no client cert is
// present (dev paths still work over plain HTTP).
package mtls

import (
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Trust is the SPIFFE trust domain we accept. Lock-step with the agent
// install template; deviating means the api silently rejects every
// snapshot.
const Trust = "optiqor.dev"

// URIPrefix is the path prefix every legitimate agent SVID carries.
const URIPrefix = "/tenant/"

var (
	ErrNoClientCert    = errors.New("mtls: no client cert presented")
	ErrNoSPIFFEUSAN    = errors.New("mtls: client cert has no SPIFFE URI SAN")
	ErrWrongTrust      = errors.New("mtls: SPIFFE trust domain mismatch")
	ErrMalformedSPIFFE = errors.New("mtls: SPIFFE URI does not encode a tenant id")
)

// ExtractTenant returns the tenancy.Context bound to the verified peer
// cert. The handler must have already accepted the cert chain via
// TLSConfig.ClientAuth = RequireAndVerifyClientCert; this function
// only parses URI SANs.
func ExtractTenant(r *http.Request) (tenancy.Context, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return tenancy.Context{}, ErrNoClientCert
	}
	cert := r.TLS.PeerCertificates[0]
	tenantID, err := tenantIDFromCert(cert)
	if err != nil {
		return tenancy.Context{}, err
	}
	return tenancy.Context{TenantID: tenantID}, nil
}

func tenantIDFromCert(cert *x509.Certificate) (string, error) {
	for _, uri := range cert.URIs {
		id, err := tenantIDFromURI(uri)
		if err == nil {
			return id, nil
		}
	}
	return "", ErrNoSPIFFEUSAN
}

func tenantIDFromURI(u *url.URL) (string, error) {
	if u == nil || u.Scheme != "spiffe" {
		return "", ErrNoSPIFFEUSAN
	}
	if !strings.EqualFold(u.Host, Trust) {
		return "", ErrWrongTrust
	}
	if !strings.HasPrefix(u.Path, URIPrefix) {
		return "", ErrMalformedSPIFFE
	}
	id := strings.TrimPrefix(u.Path, URIPrefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ErrMalformedSPIFFE
	}
	return id, nil
}
