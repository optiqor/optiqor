package argocd

import (
	"strings"
	"testing"

	"github.com/optiqor/backend/internal/parser/gitops"
)

const singleSourceManifest = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: web-prod
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/acme/charts
    targetRevision: HEAD
    path: web
    helm:
      values: |
        replicas: 3
        image: nginx:1.4.2
  destination:
    server: https://kubernetes.default.svc
    namespace: web-prod
`

const multiSourceManifest = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kafka
  namespace: argocd
spec:
  sources:
    - repoURL: https://strimzi.io/charts
      chart: strimzi-kafka-operator
      targetRevision: 0.40.0
    - repoURL: https://github.com/acme/infra
      targetRevision: main
      path: kafka/values
  destination:
    server: https://kubernetes.default.svc
    namespace: kafka
`

const irrelevantDoc = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: ignore-me
data:
  foo: bar
`

func TestParse_SingleSource(t *testing.T) {
	srcs, err := Parse(strings.NewReader(singleSourceManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(srcs) != 1 {
		t.Fatalf("got %d sources, want 1: %+v", len(srcs), srcs)
	}
	s := srcs[0]
	if s.Tool != gitops.ToolArgoCD {
		t.Errorf("Tool = %q", s.Tool)
	}
	if s.ApplicationName != "web-prod" || s.Namespace != "argocd" {
		t.Errorf("metadata lost: %+v", s)
	}
	if s.RepoURL != "https://github.com/acme/charts" || s.Path != "web" {
		t.Errorf("source fields lost: %+v", s)
	}
	if !strings.Contains(s.Values, "replicas: 3") {
		t.Errorf("inline helm values missing: %q", s.Values)
	}
	if s.DestNamespace != "web-prod" {
		t.Errorf("destination missing: %+v", s)
	}
}

func TestParse_MultiSource(t *testing.T) {
	srcs, err := Parse(strings.NewReader(multiSourceManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(srcs) != 2 {
		t.Fatalf("got %d sources, want 2", len(srcs))
	}
	if srcs[0].Chart != "strimzi-kafka-operator" || srcs[0].Path != "" {
		t.Errorf("first source = %+v", srcs[0])
	}
	if srcs[1].Path != "kafka/values" || srcs[1].Chart != "" {
		t.Errorf("second source = %+v", srcs[1])
	}
	for _, s := range srcs {
		if s.ApplicationName != "kafka" {
			t.Errorf("application name lost: %+v", s)
		}
	}
}

func TestParse_IgnoresUnrecognisedDocs(t *testing.T) {
	doc := singleSourceManifest + "\n---\n" + irrelevantDoc
	srcs, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(srcs) != 1 {
		t.Errorf("ConfigMap should be ignored; got %d sources", len(srcs))
	}
}

func TestParse_BadYAML(t *testing.T) {
	if _, err := Parse(strings.NewReader("this: is: not: valid::")); err == nil {
		t.Fatal("expected error on malformed yaml")
	}
}

func TestParse_EmptyDocument(t *testing.T) {
	srcs, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if len(srcs) != 0 {
		t.Errorf("empty input should yield 0 sources, got %d", len(srcs))
	}
}

func TestParse_UnsupportedAPIVersion(t *testing.T) {
	doc := `apiVersion: argoproj.io/v1
kind: Application
metadata: {name: x}
spec:
  source: {repoURL: https://x, path: a}
  destination: {server: x, namespace: y}
`
	srcs, _ := Parse(strings.NewReader(doc))
	if len(srcs) != 0 {
		t.Errorf("unsupported API version should be skipped; got %d", len(srcs))
	}
}

func TestParse_SourceAndSourcesBothPresent(t *testing.T) {
	// Edge case: customer migrating from single→multi source. We emit
	// `source` first, then each entry of `sources`.
	doc := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: x}
spec:
  source: {repoURL: https://primary, path: a}
  sources:
    - {repoURL: https://secondary, path: b}
  destination: {server: x, namespace: y}
`
	srcs, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(srcs))
	}
	if srcs[0].RepoURL != "https://primary" || srcs[1].RepoURL != "https://secondary" {
		t.Errorf("source ordering wrong: %+v", srcs)
	}
}
