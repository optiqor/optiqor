package operators

import "testing"

// resolverFromMap keys are "<APIVersion>/<Kind>/<Name>".
func resolverFromMap(m map[string]OwnerRef) func(OwnerRef) (OwnerRef, bool) {
	return func(o OwnerRef) (OwnerRef, bool) {
		key := o.APIVersion + "/" + o.Kind + "/" + o.Name
		next, ok := m[key]
		return next, ok
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name         string
		workload     Workload
		ownerChain   map[string]OwnerRef
		wantDirect   bool
		wantOperator string
		wantFormat   string // empty skips the FormatClass assertion
	}{
		{
			name: "direct-deployment",
			workload: Workload{
				Namespace: "default",
				Kind:      "Pod",
				Name:      "api-7c5d-xyz",
				Owners: []OwnerRef{
					{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7c5d", Controller: true},
				},
			},
			ownerChain: map[string]OwnerRef{
				"apps/v1/ReplicaSet/api-7c5d": {APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Controller: true},
				"apps/v1/Deployment/api":      {},
			},
			wantDirect: true,
			wantFormat: "direct",
		},
		{
			name: "operator-owned-strimzi",
			workload: Workload{
				Namespace: "kafka",
				Kind:      "Pod",
				Name:      "broker-0",
				Owners: []OwnerRef{
					{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "broker", Controller: true},
				},
			},
			ownerChain: map[string]OwnerRef{
				"apps/v1/StatefulSet/broker": {APIVersion: "kafka.strimzi.io/v1beta2", Kind: "Kafka", Name: "events", Controller: true},
			},
			wantDirect:   false,
			wantOperator: "kafka.strimzi.io/Kafka",
			wantFormat:   "operator:kafka.strimzi.io/Kafka",
		},
		{
			name: "operator-owned-prometheus",
			workload: Workload{
				Namespace: "monitoring",
				Kind:      "Pod",
				Name:      "prometheus-k8s-0",
				Owners: []OwnerRef{
					{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "prometheus-k8s", Controller: true},
				},
			},
			ownerChain: map[string]OwnerRef{
				"apps/v1/StatefulSet/prometheus-k8s": {APIVersion: "monitoring.coreos.com/v1", Kind: "Prometheus", Name: "k8s", Controller: true},
			},
			wantDirect:   false,
			wantOperator: "monitoring.coreos.com/Prometheus",
		},
		{
			name: "cronjob-chain-is-direct",
			workload: Workload{
				Namespace: "batch",
				Kind:      "Pod",
				Name:      "report-12345",
				Owners: []OwnerRef{
					{APIVersion: "batch/v1", Kind: "Job", Name: "report-12345", Controller: true},
				},
			},
			ownerChain: map[string]OwnerRef{
				"batch/v1/Job/report-12345": {APIVersion: "batch/v1", Kind: "CronJob", Name: "report", Controller: true},
				"batch/v1/CronJob/report":   {},
			},
			wantDirect: true,
		},
		{
			name:       "no-owners-direct",
			workload:   Workload{Kind: "Deployment", Name: "api"},
			ownerChain: nil,
			wantDirect: true,
		},
		{
			// Self-cycle: ReplicaSet a -> Deployment d -> ReplicaSet a.
			name: "cycle-yields-unknown",
			workload: Workload{
				Kind:   "Pod",
				Name:   "p",
				Owners: []OwnerRef{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: true}},
			},
			ownerChain: map[string]OwnerRef{
				"apps/v1/ReplicaSet/a": {APIVersion: "apps/v1", Kind: "Deployment", Name: "d", Controller: true},
				"apps/v1/Deployment/d": {APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: true},
			},
			wantDirect:   false,
			wantOperator: "",
		},
		{
			name: "first-controller-wins",
			workload: Workload{
				Kind: "Pod",
				Name: "p",
				Owners: []OwnerRef{
					{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: false},
					{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "b", Controller: true},
				},
			},
			ownerChain: map[string]OwnerRef{
				"apps/v1/DaemonSet/b": {},
			},
			wantDirect: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resolve func(OwnerRef) (OwnerRef, bool)
			if tc.ownerChain == nil {
				resolve = func(OwnerRef) (OwnerRef, bool) { return OwnerRef{}, false }
			} else {
				resolve = resolverFromMap(tc.ownerChain)
			}
			got := Classify(tc.workload, resolve)
			if got.Direct != tc.wantDirect {
				t.Errorf("Direct = %v, want %v (got %+v)", got.Direct, tc.wantDirect, got)
			}
			if got.Operator != tc.wantOperator {
				t.Errorf("Operator = %q, want %q", got.Operator, tc.wantOperator)
			}
			if tc.wantFormat != "" && got.FormatClass() != tc.wantFormat {
				t.Errorf("FormatClass = %q, want %q", got.FormatClass(), tc.wantFormat)
			}
		})
	}
}

func TestApiGroup(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "apps-v1", in: "apps/v1", want: "apps"},
		{name: "core-v1", in: "v1", want: ""},
		{name: "strimzi", in: "kafka.strimzi.io/v1beta2", want: "kafka.strimzi.io"},
		{name: "prometheus-operator", in: "monitoring.coreos.com/v1", want: "monitoring.coreos.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiGroup(tc.in); got != tc.want {
				t.Errorf("apiGroup(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
