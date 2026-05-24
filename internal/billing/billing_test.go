package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestWindow_Validate(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		w       Window
		wantErr bool
	}{
		{"valid one hour", Window{Start: now.Add(-time.Hour), End: now}, false},
		{"reversed", Window{Start: now, End: now.Add(-time.Hour)}, true},
		{"zero width", Window{Start: now, End: now}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if tc.wantErr && err == nil {
				t.Error("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestResult_TotalUSDCents_SkipsNonUSD(t *testing.T) {
	r := Result{Items: []LineItem{
		{TotalUSDCents: 100, Currency: "USD"},
		{TotalUSDCents: 200, Currency: "USD"},
		{TotalUSDCents: 999, Currency: "EUR"}, // caller must convert before aggregation
		{TotalUSDCents: 50, Currency: ""},     // empty defaults to USD
	}}
	if got, want := r.TotalUSDCents(), int64(350); got != want {
		t.Errorf("TotalUSDCents = %d, want %d", got, want)
	}
}

func TestRegistry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		register   []Source
		lookup     string
		wantNames  []string
		wantCloud  Cloud
		wantTier   Tier
		wantLookup bool
	}{
		{
			name:       "lookup hits registered aws-cur",
			register:   []Source{NewAWSCUR(), NewCapacity()},
			lookup:     "aws-cur",
			wantNames:  []string{"aws-cur", "capacity"},
			wantCloud:  CloudAWS,
			wantTier:   TierCloud,
			wantLookup: true,
		},
		{
			name:       "lookup hits registered capacity",
			register:   []Source{NewAWSCUR(), NewCapacity()},
			lookup:     "capacity",
			wantNames:  []string{"aws-cur", "capacity"},
			wantCloud:  Cloud(""),
			wantTier:   TierCapacity,
			wantLookup: true,
		},
		{
			name:      "lookup miss returns error",
			register:  []Source{NewAWSCUR()},
			lookup:    "nope",
			wantNames: []string{"aws-cur"},
		},
		{
			name:      "empty registry has empty names",
			register:  nil,
			lookup:    "anything",
			wantNames: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			for _, s := range tc.register {
				r.Register(s)
			}
			if got := r.Names(); !equalStrings(got, tc.wantNames) {
				t.Errorf("Names() = %v, want %v", got, tc.wantNames)
			}
			src, err := r.Lookup(tc.lookup)
			if tc.wantLookup {
				if err != nil {
					t.Fatalf("Lookup(%q): %v", tc.lookup, err)
				}
				if src.Cloud() != tc.wantCloud {
					t.Errorf("Cloud() = %q, want %q", src.Cloud(), tc.wantCloud)
				}
				if src.Tier() != tc.wantTier {
					t.Errorf("Tier() = %q, want %q", src.Tier(), tc.wantTier)
				}
				return
			}
			if err == nil {
				t.Fatalf("Lookup(%q): want error, got source %+v", tc.lookup, src)
			}
		})
	}
}

func TestRegistry_Register_PanicsOnInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed []Source
		add  Source
	}{
		{"duplicate name", []Source{NewAWSCUR()}, NewAWSCUR()},
		{"nil source", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			for _, s := range tc.seed {
				r.Register(s)
			}
			defer func() {
				if recover() == nil {
					t.Fatal("want panic, got none")
				}
			}()
			r.Register(tc.add)
		})
	}
}

func TestSource_Query_Guards(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	good := Window{Start: now.Add(-time.Hour), End: now}

	for _, tc := range []struct {
		name      string
		src       Source
		tenant    tenancy.Context
		window    Window
		wantErrIs error // checked with errors.Is when non-nil
	}{
		{"aws-cur missing tenant", NewAWSCUR(), tenancy.Context{}, good, tenancy.ErrNoTenant},
		{"aws-cur bad window", NewAWSCUR(), tenancy.Context{TenantID: "t1"}, Window{}, nil},
		{"aws-cur stub", NewAWSCUR(), tenancy.Context{TenantID: "t1"}, good, ErrNotImplemented},
		{"capacity missing tenant", NewCapacity(), tenancy.Context{}, good, tenancy.ErrNoTenant},
		{"capacity bad window", NewCapacity(), tenancy.Context{TenantID: "t1"}, Window{}, nil},
		{"capacity stub", NewCapacity(), tenancy.Context{TenantID: "t1"}, good, ErrNotImplemented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.src.Query(context.Background(), tc.tenant, tc.window)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
				t.Errorf("err = %v, want errors.Is %v", err, tc.wantErrIs)
			}
		})
	}
}

func TestCapacity_Attribution_OmitsCloudUsesCapacityTier(t *testing.T) {
	src := NewCapacity()
	if src.Cloud() != Cloud("") {
		t.Errorf("Capacity must not name a cloud; got %q", src.Cloud())
	}
	if src.Tier() != TierCapacity {
		t.Errorf("Capacity tier = %q, want %q", src.Tier(), TierCapacity)
	}
}

// equalStrings exists because reflect.DeepEqual disagrees on []string{}
// vs nil and Registry.Names returns a non-nil empty slice.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
