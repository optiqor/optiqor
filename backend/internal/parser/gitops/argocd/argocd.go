// Package argocd parses ArgoCD Application manifests (both the legacy
// single-`source` and the ≥2.6 `sources` list layouts) into
// gitops.Source values. Multi-source apps emit one Source per inner
// entry so the cost engine treats them as independent chart/dir
// analyses.
package argocd

import (
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/optiqor/optiqor/internal/parser/gitops"
)

// SupportedAPIVersions is intentionally conservative: ArgoCD adds
// fields across versions but rarely removes them.
var SupportedAPIVersions = []string{
	"argoproj.io/v1alpha1",
}

var SupportedKinds = []string{"Application"}

// Parse extracts Sources from every recognised Application in r.
// Unrecognised documents (other CRDs, Lists, comment-only) are skipped
// silently so a multi-doc bundle still parses.
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

// applicationDoc mirrors an ArgoCD Application at the depth we care
// about; extra fields are tolerated and ignored.
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

// bytesReader avoids pulling in bytes.NewReader to keep this package's
// import graph minimal.
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
