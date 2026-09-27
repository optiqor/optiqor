//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestVCSInstallations_CrossTenantIsolation(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A'), ($3, $4, 'B')`,
		tenantA, "vcs-a-"+tenantA[:8], tenantB, "vcs-b-"+tenantB[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	// Two installations: one per tenant. Distinct installation_id per
	// provider to keep the UNIQUE (provider, installation_id) constraint
	// happy.
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO vcs_installations (tenant_id, provider, installation_id, account_login, access_token_ciphertext)
		 VALUES ($1, 'github', 111, 'alice', $3),
		        ($2, 'github', 222, 'bob',   $4)`,
		tenantA, tenantB,
		[]byte{0xCA, 0xFE, 0xBA, 0xBE}, []byte{0xDE, 0xAD, 0xBE, 0xEF},
	); err != nil {
		t.Fatalf("seed installations: %v", err)
	}

	appPool := h.AppPool(ctx, t)

	for _, tc := range []struct {
		name      string
		bind      tenancy.Context
		wantLogin string
		wantCount int
	}{
		{name: "tenant A sees alice", bind: tenancy.Context{TenantID: tenantA}, wantLogin: "alice", wantCount: 1},
		{name: "tenant B sees bob", bind: tenancy.Context{TenantID: tenantB}, wantLogin: "bob", wantCount: 1},
		{name: "empty tenant sees nothing", bind: tenancy.Context{TenantID: "00000000-0000-0000-0000-000000000000"}, wantCount: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			bindQ, bindArgs, err := db.TenantBindArgs(tc.bind)
			if err != nil {
				t.Fatalf("bind args: %v", err)
			}
			if _, err := tx.Exec(ctx, bindQ, bindArgs...); err != nil {
				t.Fatalf("bind tenant: %v", err)
			}

			rows, err := tx.Query(ctx, `SELECT account_login FROM vcs_installations`)
			if err != nil {
				t.Fatalf("select: %v", err)
			}
			defer rows.Close()
			var logins []string
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					t.Fatalf("scan: %v", err)
				}
				logins = append(logins, s)
			}
			if len(logins) != tc.wantCount {
				t.Fatalf("count = %d (%v), want %d", len(logins), logins, tc.wantCount)
			}
			if tc.wantLogin != "" && logins[0] != tc.wantLogin {
				t.Errorf("login = %q, want %q", logins[0], tc.wantLogin)
			}
		})
	}
}

func TestVCSInstallations_TokenStaysCiphertext(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantID := uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'C')`, tenantID, "ct-"+tenantID[:8]); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cipher := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO vcs_installations (tenant_id, provider, installation_id, account_login, access_token_ciphertext)
		 VALUES ($1, 'github', 555, 'login', $2)`,
		tenantID, cipher); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got []byte
	if err := h.PG.QueryRow(ctx,
		`SELECT access_token_ciphertext FROM vcs_installations WHERE installation_id = 555`).
		Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != len(cipher) {
		t.Errorf("ciphertext len = %d, want %d", len(got), len(cipher))
	}
	for i := range got {
		if got[i] != cipher[i] {
			t.Errorf("ciphertext byte %d: got %x want %x", i, got[i], cipher[i])
		}
	}
}

func TestVCSInstallations_ProviderInstallationIDUnique(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	t1, t2 := uuid.NewString(), uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'X'), ($3, $4, 'Y')`,
		t1, "x-"+t1[:8], t2, "y-"+t2[:8]); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO vcs_installations (tenant_id, provider, installation_id, account_login)
		 VALUES ($1, 'github', 999, 'first')`, t1); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Same provider + installation_id under a different tenant must
	// still violate the UNIQUE constraint — installation IDs are
	// provider-global, not tenant-scoped.
	_, err := h.PG.Exec(ctx,
		`INSERT INTO vcs_installations (tenant_id, provider, installation_id, account_login)
		 VALUES ($1, 'github', 999, 'second')`, t2)
	if err == nil {
		t.Fatal("expected unique-violation on (provider, installation_id), got nil")
	}
}
