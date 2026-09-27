package argocd

import (
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/parser/gitops"
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

const unsupportedAPIVersionDoc = `apiVersion: argoproj.io/v1
kind: Application
metadata: {name: x}
spec:
  source: {repoURL: https://x, path: a}
  destination: {server: x, namespace: y}
`

// Migration case: emit `source` first, then each `sources` entry.
const sourceAndSourcesDoc = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: x}
spec:
  source: {repoURL: https://primary, path: a}
  sources:
    - {repoURL: https://secondary, path: b}
  destination: {server: x, namespace: y}
`

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, srcs []gitops.Source)
	}{
		{
			name:  "single-source",
			input: singleSourceManifest,
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
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
			},
		},
		{
			name:  "multi-source",
			input: multiSourceManifest,
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
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
			},
		},
		{
			name:  "ignores-unrecognised-docs",
			input: singleSourceManifest + "\n---\n" + irrelevantDoc,
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
				if len(srcs) != 1 {
					t.Errorf("ConfigMap should be ignored; got %d sources", len(srcs))
				}
			},
		},
		{
			name:    "bad-yaml",
			input:   "this: is: not: valid::",
			wantErr: true,
		},
		{
			name:  "empty-document",
			input: "",
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
				if len(srcs) != 0 {
					t.Errorf("empty input should yield 0 sources, got %d", len(srcs))
				}
			},
		},
		{
			name:  "unsupported-api-version-skipped",
			input: unsupportedAPIVersionDoc,
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
				if len(srcs) != 0 {
					t.Errorf("unsupported API version should be skipped; got %d", len(srcs))
				}
			},
		},
		{
			name:  "source-and-sources-both-present",
			input: sourceAndSourcesDoc,
			check: func(t *testing.T, srcs []gitops.Source) {
				t.Helper()
				if len(srcs) != 2 {
					t.Fatalf("expected 2 sources, got %d", len(srcs))
				}
				if srcs[0].RepoURL != "https://primary" || srcs[1].RepoURL != "https://secondary" {
					t.Errorf("source ordering wrong: %+v", srcs)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srcs, err := Parse(strings.NewReader(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tc.check != nil {
				tc.check(t, srcs)
			}
		})
	}
}
