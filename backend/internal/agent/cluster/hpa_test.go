package cluster

import (
	"context"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func ptr32(n int32) *int32 { return &n }

func TestHPAReader_Get_MatchesScaleTargetRef(t *testing.T) {
	cpuUtil := int32(70)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "api-hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: ptr32(2),
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "api",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							AverageUtilization: &cpuUtil,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 4,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{Type: autoscalingv2.AbleToScale, Status: "True"},
				{Type: autoscalingv2.ScalingActive, Status: "True"},
			},
		},
	}

	client := fake.NewSimpleClientset(hpa)
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newHPAReader("c1", factory)
	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	got, err := r.Get(context.Background(), tenancy.Context{TenantID: "t1"}, agentk8s.WorkloadRef{
		ClusterID: "c1",
		Namespace: "prod",
		Kind:      "Deployment",
		Name:      "api",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatalf("want HPAState, got nil")
	}
	if got.MinReplicas != 2 || got.MaxReplicas != 10 || got.CurrentReplicas != 4 {
		t.Errorf("replica fields: %+v", got)
	}
	if got.TargetCPUUtil != 70 {
		t.Errorf("cpu target = %d", got.TargetCPUUtil)
	}
	if !got.ConditionsHealthy {
		t.Errorf("conditions healthy = false")
	}
}

func TestHPAReader_Get_NoMatch_ReturnsNil(t *testing.T) {
	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	r := newHPAReader("c1", factory)
	stop := make(chan struct{})
	defer close(stop)
	factory.Start(stop)
	factory.WaitForCacheSync(stop)

	got, err := r.Get(context.Background(), tenancy.Context{}, agentk8s.WorkloadRef{Namespace: "prod", Name: "missing", Kind: "Deployment"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestConditionsHealthy_AbleToScaleFalse_ReturnsFalse(t *testing.T) {
	conds := []autoscalingv2.HorizontalPodAutoscalerCondition{
		{Type: autoscalingv2.AbleToScale, Status: "False"},
	}
	if conditionsHealthy(conds) {
		t.Error("AbleToScale=False must report unhealthy")
	}
}
