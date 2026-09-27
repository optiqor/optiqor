package gate

import (
	"context"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// ConformValidator runs a minimal schema-shape check on the post-diff
// chart. It refuses values that obviously don't match the apiVersion +
// kind contract a downstream `helm template | kubeconform` would
// reject anyway — kept lightweight here so the worker doesn't need a
// kubeconform binary at runtime. The agent's Phase-5 dryrun stage runs
// the real kubeconform against the cluster's actual API version.
type ConformValidator struct {
	// RequiredKeys are top-level chart-values keys that must survive
	// the diff (e.g. "image", "resources"). Empty means: only run the
	// generic apiVersion/kind shape check on rendered manifests.
	RequiredKeys []string
}

func (ConformValidator) Stage() Stage { return StageConform }

func (v ConformValidator) Validate(_ context.Context, _ tenancy.Context, c Candidate) StageResult {
	if strings.TrimSpace(c.ChartYAML) == "" || strings.TrimSpace(c.UnifiedDiff) == "" {
		return failed(StageConform, "empty input", errors.New("gate/conform: empty chart or diff"))
	}
	patched, err := applyUnifiedDiff(c.ChartYAML, c.UnifiedDiff)
	if err != nil {
		return failed(StageConform, "diff did not apply", err)
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal([]byte(patched), &doc); err != nil {
		return failed(StageConform, "post-diff yaml invalid", err)
	}
	for _, key := range v.RequiredKeys {
		if !hasKey(doc, key) {
			return failed(StageConform, "required key removed: "+key, errors.New("gate/conform: required key dropped"))
		}
	}
	if err := checkAPIVersionKindShape(doc); err != nil {
		return failed(StageConform, "manifest shape rejected", err)
	}
	return StageResult{Stage: StageConform, Status: StatusPassed}
}

func hasKey(node any, key string) bool {
	switch n := node.(type) {
	case map[string]any:
		if _, ok := n[key]; ok {
			return true
		}
		for _, v := range n {
			if hasKey(v, key) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if hasKey(v, key) {
				return true
			}
		}
	}
	return false
}

// checkAPIVersionKindShape walks any "apiVersion: ... kind: ..." pairs
// the values file declares and rejects the well-known typos that would
// blow up kubeconform: empty apiVersion, kind without group/version,
// or kind set to an empty string.
func checkAPIVersionKindShape(node any) error {
	switch n := node.(type) {
	case map[string]any:
		_, hasAPI := n["apiVersion"]
		_, hasKind := n["kind"]
		if hasAPI || hasKind {
			api, _ := n["apiVersion"].(string)
			kind, _ := n["kind"].(string)
			if hasAPI && api == "" {
				return errors.New("gate/conform: apiVersion is empty")
			}
			if hasKind && kind == "" {
				return errors.New("gate/conform: kind is empty")
			}
		}
		for _, v := range n {
			if err := checkAPIVersionKindShape(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range n {
			if err := checkAPIVersionKindShape(v); err != nil {
				return err
			}
		}
	}
	return nil
}
