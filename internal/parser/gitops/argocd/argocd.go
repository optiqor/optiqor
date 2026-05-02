// Package argocd parses ArgoCD `Application` (and `ApplicationSet`)
// custom resources into the normalised gitops.Source shape.
//
// ArgoCD has two manifest layouts in the wild:
//
//  1. Single-source: `spec.source` is a map.
//  2. Multi-source: `spec.sources` is a list (ArgoCD ≥ 2.6).
//
// We support both and emit one gitops.Source per inner source so the
// downstream cost engine treats the multi-source case as N independent
// chart/dir analyses.
package argocd

import (
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/optiqor/backend/internal/parser/gitops"
)

// SupportedAPIVersions lists the ArgoCD API versions this reader
// accepts. Future ArgoCD versions add fields, not (typically) remove
// them, so this is conservative.
var SupportedAPIVersions = []string{
	"argoproj.io/v1alpha1",
}

// SupportedKinds lists the resource kinds this reader recognises.
var SupportedKinds = []string{"Application"}

// Parse reads one or more YAML documents from r and returns every
// `Source` extracted from any `Application` it finds. Documents whose
// kind is not in SupportedKinds are skipped silently — they may be
// other CRDs, Lists, or comments-only documents.
func Parse(r io.Reader) ([]gitops.Source, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("argocd: read: %w", err)
	}
	dec := yaml.NewDecoder(bytesReader(raw))
	var out []gitops.Source
	for {
		var doc applicationDoc
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("argocd: yaml: %w", err)
		}
		if !doc.recognised() {
			continue
		}
		out = append(out, doc.sources()...)
	}
	return out, nil
}

// applicationDoc mirrors the shape of an ArgoCD Application manifest
// at the depth we care about. Fields outside this set are tolerated
// but ignored.
type applicationDoc struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   metadata        `yaml:"metadata"`
	Spec       applicationSpec `yaml:"spec"`
}

type metadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type applicationSpec struct {
	Source      *sourceSpec     `yaml:"source"`
	Sources     []sourceSpec    `yaml:"sources"`
	Destination destinationSpec `yaml:"destination"`
}

type sourceSpec struct {
	RepoURL        string    `yaml:"repoURL"`
	TargetRevision string    `yaml:"targetRevision"`
	Path           string    `yaml:"path"`
	Chart          string    `yaml:"chart"`
	Helm           *helmSpec `yaml:"helm"`
}

type helmSpec struct {
	Values     string   `yaml:"values"`
	ValueFiles []string `yaml:"valueFiles"`
}

type destinationSpec struct {
	Server    string `yaml:"server"`
	Namespace string `yaml:"namespace"`
}

func (d applicationDoc) recognised() bool {
	if d.Kind != "Application" {
		return false
	}
	for _, v := range SupportedAPIVersions {
		if d.APIVersion == v {
			return true
		}
	}
	return false
}

func (d applicationDoc) sources() []gitops.Source {
	specs := d.Spec.Sources
	if d.Spec.Source != nil {
		specs = append([]sourceSpec{*d.Spec.Source}, specs...)
	}
	out := make([]gitops.Source, 0, len(specs))
	for _, s := range specs {
		src := gitops.Source{
			Tool:            gitops.ToolArgoCD,
			ApplicationName: d.Metadata.Name,
			Namespace:       d.Metadata.Namespace,
			RepoURL:         s.RepoURL,
			TargetRevision:  s.TargetRevision,
			Path:            s.Path,
			Chart:           s.Chart,
			DestServer:      d.Spec.Destination.Server,
			DestNamespace:   d.Spec.Destination.Namespace,
		}
		if s.Helm != nil {
			src.Values = s.Helm.Values
		}
		out = append(out, src)
	}
	return out
}

// bytesReader is a tiny adapter so yaml.NewDecoder can read from a
// byte slice without reaching for bytes.NewReader (we want to keep
// this package's import graph small).
func bytesReader(b []byte) io.Reader { return &readerImpl{b: b} }

type readerImpl struct {
	b []byte
	i int
}

func (r *readerImpl) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
