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

func TestClassify_DirectDeployment(t *testing.T) {
	w := Workload{
		Namespace: "default",
		Kind:      "Pod",
		Name:      "api-7c5d-xyz",
		Owners: []OwnerRef{
			{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7c5d", Controller: true},
		},
	}
	resolve := resolverFromMap(map[string]OwnerRef{
		"apps/v1/ReplicaSet/api-7c5d": {APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Controller: true},
		"apps/v1/Deployment/api":      {},
	})
	got := Classify(w, resolve)
	if !got.Direct {
		t.Fatalf("got %+v, want Direct", got)
	}
	if got.FormatClass() != "direct" {
		t.Errorf("FormatClass = %q", got.FormatClass())
	}
}

func TestClassify_OperatorOwned_Strimzi(t *testing.T) {
	w := Workload{
		Namespace: "kafka",
		Kind:      "Pod",
		Name:      "broker-0",
		Owners: []OwnerRef{
			{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "broker", Controller: true},
		},
	}
	resolve := resolverFromMap(map[string]OwnerRef{
		"apps/v1/StatefulSet/broker": {APIVersion: "kafka.strimzi.io/v1beta2", Kind: "Kafka", Name: "events", Controller: true},
	})
	got := Classify(w, resolve)
	if got.Direct {
		t.Fatalf("expected operator-owned: %+v", got)
	}
	if got.Operator != "kafka.strimzi.io/Kafka" {
		t.Errorf("Operator = %q", got.Operator)
	}
	if got.FormatClass() != "operator:kafka.strimzi.io/Kafka" {
		t.Errorf("FormatClass = %q", got.FormatClass())
	}
}

func TestClassify_OperatorOwned_PrometheusOperator(t *testing.T) {
	w := Workload{
		Namespace: "monitoring",
		Kind:      "Pod",
		Name:      "prometheus-k8s-0",
		Owners: []OwnerRef{
			{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "prometheus-k8s", Controller: true},
		},
	}
	resolve := resolverFromMap(map[string]OwnerRef{
		"apps/v1/StatefulSet/prometheus-k8s": {APIVersion: "monitoring.coreos.com/v1", Kind: "Prometheus", Name: "k8s", Controller: true},
	})
	got := Classify(w, resolve)
	if got.Direct {
		t.Fatal("expected operator-owned")
	}
	if got.Operator != "monitoring.coreos.com/Prometheus" {
		t.Errorf("Operator = %q", got.Operator)
	}
}

func TestClassify_CronJobChainIsDirect(t *testing.T) {
	w := Workload{
		Namespace: "batch",
		Kind:      "Pod",
		Name:      "report-12345",
		Owners: []OwnerRef{
			{APIVersion: "batch/v1", Kind: "Job", Name: "report-12345", Controller: true},
		},
	}
	resolve := resolverFromMap(map[string]OwnerRef{
		"batch/v1/Job/report-12345": {APIVersion: "batch/v1", Kind: "CronJob", Name: "report", Controller: true},
		"batch/v1/CronJob/report":   {},
	})
	got := Classify(w, resolve)
	if !got.Direct {
		t.Fatalf("CronJob chain should be direct: %+v", got)
	}
}

func TestClassify_NoOwners_Direct(t *testing.T) {
	w := Workload{Kind: "Deployment", Name: "api"}
	got := Classify(w, func(OwnerRef) (OwnerRef, bool) { return OwnerRef{}, false })
	if !got.Direct {
		t.Errorf("Deployment with no owners should be direct: %+v", got)
	}
}

func TestClassify_CycleGuard(t *testing.T) {
	w := Workload{
		Kind:   "Pod",
		Name:   "p",
		Owners: []OwnerRef{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: true}},
	}
	// Self-cycle: ReplicaSet a → Deployment d → ReplicaSet a (cycle).
	resolve := resolverFromMap(map[string]OwnerRef{
		"apps/v1/ReplicaSet/a": {APIVersion: "apps/v1", Kind: "Deployment", Name: "d", Controller: true},
		"apps/v1/Deployment/d": {APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: true},
	})
	got := Classify(w, resolve)
	if got.Direct || got.Operator != "" {
		t.Errorf("cycle should yield unknown: %+v", got)
	}
}

func TestClassify_FirstControllerWins(t *testing.T) {
	w := Workload{
		Kind: "Pod",
		Name: "p",
		Owners: []OwnerRef{
			{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "a", Controller: false},
			{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "b", Controller: true},
		},
	}
	resolve := resolverFromMap(map[string]OwnerRef{
		"apps/v1/DaemonSet/b": {},
	})
	got := Classify(w, resolve)
	if !got.Direct {
		t.Fatalf("controller=true (DaemonSet) should win over the first non-controller owner; got %+v", got)
	}
}

func TestApiGroup(t *testing.T) {
	cases := map[string]string{
		"apps/v1":                  "apps",
		"v1":                       "",
		"kafka.strimzi.io/v1beta2": "kafka.strimzi.io",
		"monitoring.coreos.com/v1": "monitoring.coreos.com",
	}
	for in, want := range cases {
		if got := apiGroup(in); got != want {
			t.Errorf("apiGroup(%q) = %q, want %q", in, got, want)
		}
	}
}
