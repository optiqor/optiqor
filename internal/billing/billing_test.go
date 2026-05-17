package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestWindow_Validate(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		w       Window
		wantErr bool
	}{
		{"valid", Window{Start: now.Add(-time.Hour), End: now}, false},
		{"reversed", Window{Start: now, End: now.Add(-time.Hour)}, true},
		{"zero", Window{Start: now, End: now}, true},
	}
	for _, tc := range cases {
		err := tc.w.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestResult_TotalUSDCentsSkipsNonUSD(t *testing.T) {
	r := Result{Items: []LineItem{
		{TotalUSDCents: 100, Currency: "USD"},
		{TotalUSDCents: 200, Currency: "USD"},
		{TotalUSDCents: 999, Currency: "EUR"}, // skipped — caller must convert first
		{TotalUSDCents: 50, Currency: ""},     // empty defaults to USD
	}}
	if got, want := r.TotalUSDCents(), int64(350); got != want {
		t.Errorf("TotalUSDCents = %d, want %d", got, want)
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := newRegistry()
	r.Register(NewAWSCUR())
	r.Register(NewCapacity())

	if names := r.Names(); len(names) != 2 || names[0] != "aws-cur" || names[1] != "capacity" {
		t.Fatalf("Names() = %v, want [aws-cur capacity]", names)
	}

	src, err := r.Lookup("aws-cur")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if src.Cloud() != CloudAWS || src.Tier() != TierCloud {
		t.Errorf("aws source attrs wrong: %+v %+v", src.Cloud(), src.Tier())
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	r := newRegistry()
	if _, err := r.Lookup("nope"); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestRegistry_RegisterDuplicatePanics(t *testing.T) {
	r := newRegistry()
	r.Register(NewAWSCUR())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate register")
		}
	}()
	r.Register(NewAWSCUR())
}

func TestRegistry_RegisterNilPanics(t *testing.T) {
	r := newRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil source")
		}
	}()
	r.Register(nil)
}

func TestAWSCUR_QueryGuards(t *testing.T) {
	src := NewAWSCUR()
	now := time.Now()
	good := Window{Start: now.Add(-time.Hour), End: now}

	// no tenant
	if _, err := src.Query(context.Background(), tenancy.Context{}, good); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("expected ErrNoTenant, got %v", err)
	}
	// invalid window
	if _, err := src.Query(context.Background(), tenancy.Context{TenantID: "t1"}, Window{}); err == nil {
		t.Error("expected window-validation error")
	}
	// happy path returns ErrNotImplemented in Phase 1
	if _, err := src.Query(context.Background(), tenancy.Context{TenantID: "t1"}, good); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestCapacity_QueryGuards(t *testing.T) {
	src := NewCapacity()
	now := time.Now()
	good := Window{Start: now.Add(-time.Hour), End: now}

	if _, err := src.Query(context.Background(), tenancy.Context{}, good); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("expected ErrNoTenant, got %v", err)
	}
	if _, err := src.Query(context.Background(), tenancy.Context{TenantID: "t1"}, good); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
	if src.Cloud() != Cloud("") {
		t.Errorf("Capacity must not name a cloud; got %q", src.Cloud())
	}
	if src.Tier() != TierCapacity {
		t.Errorf("Capacity tier wrong: %q", src.Tier())
	}
}
