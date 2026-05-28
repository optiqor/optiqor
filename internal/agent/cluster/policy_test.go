package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestPolicyReader_Snapshot_AggregatesPDBQuotaLimitRange(t *testing.T) {
	pdbMin := intstr.FromInt(2)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "api-pdb"},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &pdbMin,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
			},
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy: 3,
		},
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "prod-quota"},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("16"),
				corev1.ResourceLimitsMemory: resource.MustParse("32Gi"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{
				corev1.ResourceLimitsCPU:    resource.MustParse("4"),
				corev1.ResourceLimitsMemory: resource.MustParse("8Gi"),
			},
		},
	}

	limitRange := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "prod-limits"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
					Min: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pdb, quota, limitRange)
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newPolicyReader("c1", factory)
	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	got, err := r.Snapshot(context.Background(), tenancy.Context{TenantID: "t1"}, agentk8s.WorkloadRef{
		ClusterID: "c1",
		Namespace: "prod",
		Kind:      "Deployment",
		Name:      "api",
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.PDB == nil || got.PDB.MinAvailable != 2 || got.PDB.CurrentReplicas != 3 {
		t.Errorf("pdb: %+v", got.PDB)
	}
	if got.Quota == nil {
		t.Fatal("quota nil")
	}
	if got.Quota.CPUMillicores != 16_000 {
		t.Errorf("quota cpu = %d, want 16000", got.Quota.CPUMillicores)
	}
	if got.Quota.MemoryBytes != 32*1024*1024*1024 {
		t.Errorf("quota mem = %d", got.Quota.MemoryBytes)
	}
	if got.LimitRange == nil {
		t.Fatal("limitrange nil")
	}
	if got.LimitRange.MaxCPUMillicores != 4_000 {
		t.Errorf("limitrange max cpu = %d", got.LimitRange.MaxCPUMillicores)
	}
	if got.LimitRange.MinMemoryBytes != 128*1024*1024 {
		t.Errorf("limitrange min mem = %d", got.LimitRange.MinMemoryBytes)
	}
}

func TestPolicyReader_Snapshot_EmptyNamespace_ReturnsAllNil(t *testing.T) {
	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newPolicyReader("c1", factory)
	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	got, err := r.Snapshot(context.Background(), tenancy.Context{}, agentk8s.WorkloadRef{Namespace: "empty"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.PDB != nil || got.Quota != nil || got.LimitRange != nil {
		t.Errorf("want all nil, got %+v", got)
	}
}

func TestPolicyReader_MultipleLimitRanges_PicksNarrowestEnvelope(t *testing.T) {
	wide := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "wide"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer,
				Max:  corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")},
			}},
		},
	}
	narrow := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "narrow"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer,
				Max:  corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			}},
		},
	}

	client := fake.NewSimpleClientset(wide, narrow)
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newPolicyReader("c1", factory)
	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	got, err := r.Snapshot(context.Background(), tenancy.Context{}, agentk8s.WorkloadRef{Namespace: "prod"})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.LimitRange == nil || got.LimitRange.MaxCPUMillicores != 2_000 {
		t.Errorf("want narrowest 2000m, got %+v", got.LimitRange)
	}
}
