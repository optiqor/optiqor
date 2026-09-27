package provisioner

import (
	"context"
	"errors"
	"testing"
)

type fakeProbe struct {
	karp, ca       bool
	karpErr, caErr error
}

func (f *fakeProbe) HasKarpenterCRD(_ context.Context) (bool, error) {
	return f.karp, f.karpErr
}
func (f *fakeProbe) HasClusterAutoscalerDeployment(_ context.Context) (bool, error) {
	return f.ca, f.caErr
}

func TestDetect_KarpenterWinsOverCA(t *testing.T) {
	c, err := Detect(context.Background(), &fakeProbe{karp: true, ca: true})
	if err != nil || c != ClassKarpenter {
		t.Errorf("class = %q err = %v", c, err)
	}
}

func TestDetect_OnlyCA(t *testing.T) {
	c, err := Detect(context.Background(), &fakeProbe{ca: true})
	if err != nil || c != ClassAutoscaler {
		t.Errorf("class = %q err = %v", c, err)
	}
}

func TestDetect_Neither_Static(t *testing.T) {
	c, err := Detect(context.Background(), &fakeProbe{})
	if err != nil || c != ClassStatic {
		t.Errorf("class = %q err = %v", c, err)
	}
}

func TestDetect_KarpenterProbeError_StaticAndError(t *testing.T) {
	c, err := Detect(context.Background(), &fakeProbe{karpErr: errors.New("timeout")})
	if err == nil || c != ClassStatic {
		t.Errorf("want (static, err); got (%q, %v)", c, err)
	}
}

func TestDetect_NilProbe(t *testing.T) {
	c, err := Detect(context.Background(), nil)
	if err == nil || c != ClassStatic {
		t.Errorf("want (static, err); got (%q, %v)", c, err)
	}
}

func TestAdvisoryNote_PerClass(t *testing.T) {
	if AdvisoryNote(ClassKarpenter) != "" {
		t.Error("Karpenter should have no advisory")
	}
	if AdvisoryNote(ClassAutoscaler) == "" {
		t.Error("autoscaler should mention ASG")
	}
	if AdvisoryNote(ClassStatic) == "" {
		t.Error("static should mention manual step")
	}
}

func TestConfidenceCap_OnlyStaticCaps(t *testing.T) {
	if ConfidenceCap(ClassStatic) != "medium" {
		t.Error("static must cap at medium")
	}
	if ConfidenceCap(ClassKarpenter) != "" || ConfidenceCap(ClassAutoscaler) != "" {
		t.Error("T1/T2 must not cap")
	}
}
