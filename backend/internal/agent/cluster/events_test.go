package cluster

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestEventsReader_Recent_FiltersBySinceAndWorkload(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	objs := []*corev1.Event{
		{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "prod", Name: "ev-old"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "api-abc-xyz"},
			Reason:         "OOMKilled",
			Type:           "Warning",
			Count:          1,
			LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Hour)),
			FirstTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "prod", Name: "ev-fresh"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "api-abc-xyz"},
			Reason:         "FailedScheduling",
			Type:           "Warning",
			Count:          3,
			LastTimestamp:  metav1.NewTime(now.Add(-5 * time.Minute)),
			FirstTimestamp: metav1.NewTime(now.Add(-30 * time.Minute)),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "prod", Name: "ev-other-ns"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "staging", Name: "api-abc-xyz"},
			Reason:         "OOMKilled",
			LastTimestamp:  metav1.NewTime(now),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Namespace: "prod", Name: "ev-other-workload"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "worker-zzz-yyy"},
			Reason:         "OOMKilled",
			LastTimestamp:  metav1.NewTime(now),
		},
	}

	objRefs := make([]runtime.Object, len(objs))
	for i, o := range objs {
		objRefs[i] = o
	}
	client := fake.NewSimpleClientset(objRefs...)
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newEventsReader("c1", factory)

	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	since := now.Add(-1 * time.Hour)
	got, err := r.Recent(context.Background(), tenancy.Context{TenantID: "11111111-1111-1111-1111-111111111111"}, agentk8s.WorkloadRef{
		ClusterID: "c1",
		Namespace: "prod",
		Kind:      "Pod",
		Name:      "api",
	}, since)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(got), got)
	}
	if got[0].Reason != "FailedScheduling" {
		t.Errorf("filtered to wrong event: %+v", got[0])
	}
	if got[0].Count != 3 {
		t.Errorf("count not propagated: %+v", got[0])
	}
}

func TestEventsReader_NilIndexer_ReturnsEmpty(t *testing.T) {
	r := &EventsR{}
	got, err := r.Recent(context.Background(), tenancy.Context{}, agentk8s.WorkloadRef{}, time.Time{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestMatchesWorkloadName_StripsReplicaSetHashAndPodHash(t *testing.T) {
	for _, tc := range []struct {
		name     string
		involved string
		want     string
		match    bool
	}{
		{"exact", "api", "api", true},
		{"rs-hash", "api-7c4f8b6d4", "api", true},
		{"pod-hash", "api-7c4f8b6d4-xyz12", "api", true},
		{"deep-mismatch", "worker-zzz-yyy", "api", false},
		{"prefix-only", "apiserver", "api", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesWorkloadName(tc.involved, tc.want); got != tc.match {
				t.Errorf("matchesWorkloadName(%q, %q) = %v, want %v", tc.involved, tc.want, got, tc.match)
			}
		})
	}
}
