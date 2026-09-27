package graph

import (
	"context"
	"reflect"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type fakeReader struct {
	svcs []Service
	eps  []EndpointSlice
	wls  map[WorkloadKey]map[string]string
}

func (f *fakeReader) Services(_ context.Context, _ tenancy.Context) ([]Service, error) {
	return f.svcs, nil
}

func (f *fakeReader) Endpoints(_ context.Context, _ tenancy.Context) ([]EndpointSlice, error) {
	return f.eps, nil
}

func (f *fakeReader) WorkloadLabels(_ context.Context, _ tenancy.Context) (map[WorkloadKey]map[string]string, error) {
	return f.wls, nil
}

func TestBuild_LivenessFromReadyAddresses(t *testing.T) {
	r := &fakeReader{
		wls: map[WorkloadKey]map[string]string{
			{Namespace: "prod", Kind: "Deployment", Name: "api"}:    {"app": "api"},
			{Namespace: "prod", Kind: "Deployment", Name: "worker"}: {"app": "worker"},
		},
		svcs: []Service{
			{Namespace: "prod", Name: "api-svc", Selector: map[string]string{"app": "api"}},
			{Namespace: "prod", Name: "worker-svc", Selector: map[string]string{"app": "worker"}},
		},
		eps: []EndpointSlice{
			{Namespace: "prod", Service: "api-svc", ReadyAddresses: 3},
			{Namespace: "prod", Service: "worker-svc", ReadyAddresses: 0},
		},
	}

	snap, err := Build(context.Background(), r, tenancy.Context{TenantID: "t1"}, 1234567890)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if snap.CapturedAt != 1234567890 {
		t.Errorf("captured = %d", snap.CapturedAt)
	}
	api := WorkloadKey{Namespace: "prod", Kind: "Deployment", Name: "api"}
	wkr := WorkloadKey{Namespace: "prod", Kind: "Deployment", Name: "worker"}
	if !snap.Liveness[api] {
		t.Error("api should be live (3 ready endpoints)")
	}
	if snap.Liveness[wkr] {
		t.Error("worker should NOT be live (0 ready endpoints) — scale-to-zero candidate")
	}
}

func TestBuild_DeterministicOrdering(t *testing.T) {
	r := &fakeReader{
		wls: map[WorkloadKey]map[string]string{
			{Namespace: "prod", Kind: "Deployment", Name: "z-svc"}: {"app": "z"},
			{Namespace: "prod", Kind: "Deployment", Name: "a-svc"}: {"app": "a"},
			{Namespace: "dev", Kind: "Deployment", Name: "m-svc"}:  {"app": "m"},
		},
	}
	a, _ := Build(context.Background(), r, tenancy.Context{}, 1)
	b, _ := Build(context.Background(), r, tenancy.Context{}, 1)
	if !reflect.DeepEqual(a.Workloads, b.Workloads) {
		t.Errorf("non-deterministic order: %+v vs %+v", a.Workloads, b.Workloads)
	}
	// dev comes before prod alphabetically
	if a.Workloads[0].Namespace != "dev" {
		t.Errorf("expected dev first; got %+v", a.Workloads)
	}
}

func TestBuild_EmptyInput_ReturnsEmptySnapshot(t *testing.T) {
	r := &fakeReader{wls: map[WorkloadKey]map[string]string{}}
	snap, err := Build(context.Background(), r, tenancy.Context{}, 0)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(snap.Workloads) != 0 || len(snap.Edges) != 0 {
		t.Errorf("want empty; got %+v", snap)
	}
}

func TestBuild_SelectorWithoutAllLabels_DoesNotMatch(t *testing.T) {
	r := &fakeReader{
		wls: map[WorkloadKey]map[string]string{
			{Namespace: "prod", Kind: "Deployment", Name: "api"}: {"app": "api", "tier": "frontend"},
		},
		svcs: []Service{
			{Namespace: "prod", Name: "api-svc", Selector: map[string]string{"app": "api", "tier": "backend"}},
		},
		eps: []EndpointSlice{{Namespace: "prod", Service: "api-svc", ReadyAddresses: 1}},
	}
	snap, _ := Build(context.Background(), r, tenancy.Context{}, 0)
	if len(snap.Edges) != 0 {
		t.Errorf("selector mismatch should produce no edges; got %+v", snap.Edges)
	}
}
