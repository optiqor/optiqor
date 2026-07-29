package parser

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"gopkg.in/yaml.v3"
)

// Workload is the normalised view of one resource-bearing unit found in
// a Helm values file. Intentionally cloud-agnostic and decoupled from
// the Kubernetes API: the same shape supports Deployment, StatefulSet,
// CronJob, etc.
type Workload struct {
	Name     string
	Kind     string
	Requests ResourceList
	Limits   ResourceList
	Image    ImageRef
	// Replicas mirrors the chart's `replicas` or `replicaCount` value.
	// Zero means "unset"; detectors treat unset as the chart's default
	// (not necessarily 1).
	Replicas int
	// HasHPA reports whether an autoscaler block was declared. A bare
	// `autoscaling` mapping without `enabled: false` counts as enabled.
	HasHPA bool
	// Security captures the security-context flags the security
	// detectors care about. Nil pointers mean "not declared"; detectors
	// decide whether unset is safe.
	Security SecurityContext
}

// SecurityContext is the subset of pod / container securityContext
// fields Optiqor inspects. Nil pointers preserve "not declared" — a
// workload with runAsNonRoot=false is materially different from one
// that omits the field.
type SecurityContext struct {
	RunAsNonRoot             *bool
	RunAsUser                *int64
	Privileged               *bool
	ReadOnlyRootFilesystem   *bool
	HostNetwork              *bool
	HostPID                  *bool
	HostIPC                  *bool
	HostPath                 *bool // any volume sets hostPath
	AllowPrivilegeEscalation *bool
	// AutomountServiceAccountToken: nil means k8s default (true);
	// pointer-false means the chart explicitly disabled the auto-mount.
	AutomountServiceAccountToken *bool
	// CapabilitiesAdd / CapabilitiesDrop are case-preserving lists.
	// "ALL" in CapabilitiesDrop is treated as "drop everything".
	CapabilitiesAdd  []string
	CapabilitiesDrop []string
}

// ResourceList captures the CPU and memory of either requests or limits.
type ResourceList struct {
	CPU    Quantity
	Memory Quantity
}

// ImageRef captures the container image declared on a workload. Helm
// charts use two patterns:
//
//	image: nginx:1.4.2                      -- single string
//	image:
//	  repository: nginx                     -- map with keys
//	  tag: "1.4.2"
//
// Both are accepted. Tag == "" indicates a missing/implicit tag, which
// Kubernetes treats as :latest.
type ImageRef struct {
	Repository string
	Tag        string
	Set        bool
}

// String renders the ImageRef as repository:tag (or repository when no tag).
func (i ImageRef) String() string {
	if !i.Set {
		return ""
	}
	if i.Tag == "" {
		return i.Repository
	}
	return i.Repository + ":" + i.Tag
}

// ParseValues reads a Helm values.yaml stream and returns every workload
// candidate found. A "workload" is any nested map containing a
// `resources` key with `requests` and/or `limits`.
//
// Phase 1 supports the common pattern where chart authors expose a
// `<workload>.resources` block per service; sub-chart and template
// rendering land in Phase 2.
func ParseValues(r io.Reader) ([]Workload, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("parser: read: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parser: yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, errors.New("parser: empty document")
	}
	if root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("parser: top-level must be a map")
	}

	var workloads []Workload
	walk(root.Content[0], "", &workloads)
	sort.Slice(workloads, func(i, j int) bool { return workloads[i].Name < workloads[j].Name })
	return workloads, nil
}

func walk(n *yaml.Node, path string, out *[]Workload) {
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		childPath := joinPath(path, k.Value)

		// Treat any mapping with a resources.requests or resources.limits
		// child as a workload.
		if v.Kind == yaml.MappingNode {
			if res := findChild(v, "resources"); res != nil && res.Kind == yaml.MappingNode {
				wl := Workload{Name: childPath, Kind: "Deployment"}
				if reqs := findChild(res, "requests"); reqs != nil {
					wl.Requests = readResourceList(reqs)
				}
				if lims := findChild(res, "limits"); lims != nil {
					wl.Limits = readResourceList(lims)
				}
				if img := findChild(v, "image"); img != nil {
					wl.Image = readImage(img)
				}
				wl.Replicas = readReplicas(v)
				wl.HasHPA = readHasHPA(v)
				wl.Security = readSecurity(v)
				*out = append(*out, wl)
			}
			walk(v, childPath, out)
		}
	}
}

// readResourceList extracts cpu/memory under a requests/limits mapping.
// Unparseable values are dropped silently — the rule engine surfaces
// missing fields, not malformed ones (those are a chart-author bug).
func readResourceList(n *yaml.Node) ResourceList {
	var rl ResourceList
	if n.Kind != yaml.MappingNode {
		return rl
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "cpu":
			if q, err := ParseCPU(v.Value); err == nil {
				rl.CPU = q
			}
		case "memory":
			if q, err := ParseMemory(v.Value); err == nil {
				rl.Memory = q
			}
		}
	}
	return rl
}

// readImage handles both Helm patterns: scalar `image: repo:tag` or
// mapping `image: {repository: ..., tag: ...}`.
func readImage(n *yaml.Node) ImageRef {
	if n == nil {
		return ImageRef{}
	}
	switch n.Kind {
	case yaml.ScalarNode:
		s := n.Value
		if s == "" {
			return ImageRef{}
		}
		// Rightmost ':' splits repo from tag; we don't care about ports
		// here since this is only used for tag presence.
		repo, tag := splitImage(s)
		return ImageRef{Repository: repo, Tag: tag, Set: true}
	case yaml.MappingNode:
		ref := ImageRef{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
				continue
			}
			switch k.Value {
			case "repository", "name":
				ref.Repository = v.Value
				ref.Set = true
			case "tag":
				ref.Tag = v.Value
				ref.Set = true
			}
		}
		return ref
	}
	return ImageRef{}
}

// splitImage parses "repo:tag" → ("repo", "tag"); returns (s, "") when
// no tag is present.
func splitImage(s string) (repo, tag string) {
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			idx = i
		}
	}
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

func findChild(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v
		}
	}
	return nil
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// readReplicas accepts both `replicas` and `replicaCount` (Bitnami
// convention). Returns 0 if the field is missing or non-numeric.
func readReplicas(n *yaml.Node) int {
	for _, key := range []string{"replicas", "replicaCount"} {
		if c := findChild(n, key); c != nil && c.Kind == yaml.ScalarNode {
			v := 0
			for i := 0; i < len(c.Value); i++ {
				ch := c.Value[i]
				if ch < '0' || ch > '9' {
					return 0
				}
				v = v*10 + int(ch-'0')
			}
			return v
		}
	}
	return 0
}

// readHasHPA returns true when `autoscaling.enabled` is set or the
// `hpa` mapping is present and not disabled. A bare `autoscaling` block
// without `enabled: false` counts as enabled.
func readHasHPA(n *yaml.Node) bool {
	for _, key := range []string{"autoscaling", "hpa", "horizontalPodAutoscaler"} {
		c := findChild(n, key)
		if c == nil {
			continue
		}
		if c.Kind != yaml.MappingNode {
			continue
		}
		if en := findChild(c, "enabled"); en != nil && en.Kind == yaml.ScalarNode {
			if en.Value == "true" || en.Value == "yes" || en.Value == "1" {
				return true
			}
			if en.Value == "false" || en.Value == "no" || en.Value == "0" {
				return false
			}
		}
		return true // mapping present, no explicit disable
	}
	return false
}

// readSecurity reads `securityContext` (pod or container level) plus
// host-namespace flags, volumes, and service-account-token automount.
// The walker only inspects the workload's direct map; deep template
// rendering is Phase 7.
func readSecurity(n *yaml.Node) SecurityContext {
	var sec SecurityContext

	for _, key := range [][2]string{
		{"hostNetwork", "HostNetwork"},
		{"hostPID", "HostPID"},
		{"hostIPC", "HostIPC"},
	} {
		if v := findChild(n, key[0]); v != nil && v.Kind == yaml.ScalarNode {
			b := boolValue(v.Value)
			switch key[1] {
			case "HostNetwork":
				sec.HostNetwork = &b
			case "HostPID":
				sec.HostPID = &b
			case "HostIPC":
				sec.HostIPC = &b
			}
		}
	}

	if amt := findChild(n, "automountServiceAccountToken"); amt != nil && amt.Kind == yaml.ScalarNode {
		b := boolValue(amt.Value)
		sec.AutomountServiceAccountToken = &b
	}

	if vols := findChild(n, "volumes"); vols != nil && vols.Kind == yaml.SequenceNode {
		for _, v := range vols.Content {
			if v.Kind == yaml.MappingNode {
				if findChild(v, "hostPath") != nil {
					t := true
					sec.HostPath = &t
					break
				}
			}
		}
	}

	for _, key := range []string{"securityContext", "podSecurityContext", "containerSecurityContext"} {
		ctx := findChild(n, key)
		if ctx == nil || ctx.Kind != yaml.MappingNode {
			continue
		}
		applySecFields(ctx, &sec)
	}
	return sec
}

func applySecFields(ctx *yaml.Node, out *SecurityContext) {
	for _, key := range [][2]string{
		{"hostNetwork", "HostNetwork"},
		{"hostPID", "HostPID"},
		{"hostIPC", "HostIPC"},
	} {
		if v := findChild(ctx, key[0]); v != nil && v.Kind == yaml.ScalarNode {
			b := boolValue(v.Value)
			switch key[1] {
			case "HostNetwork":
				if out.HostNetwork == nil {
					out.HostNetwork = &b
				}
			case "HostPID":
				if out.HostPID == nil {
					out.HostPID = &b
				}
			case "HostIPC":
				if out.HostIPC == nil {
					out.HostIPC = &b
				}
			}
		}
	}

	if v := findChild(ctx, "runAsNonRoot"); v != nil && v.Kind == yaml.ScalarNode {
		b := boolValue(v.Value)
		out.RunAsNonRoot = &b
	}
	if v := findChild(ctx, "runAsUser"); v != nil && v.Kind == yaml.ScalarNode {
		n, ok := parseInt64(v.Value)
		if ok {
			out.RunAsUser = &n
		}
	}
	if v := findChild(ctx, "privileged"); v != nil && v.Kind == yaml.ScalarNode {
		b := boolValue(v.Value)
		out.Privileged = &b
	}
	if v := findChild(ctx, "readOnlyRootFilesystem"); v != nil && v.Kind == yaml.ScalarNode {
		b := boolValue(v.Value)
		out.ReadOnlyRootFilesystem = &b
	}
	if v := findChild(ctx, "allowPrivilegeEscalation"); v != nil && v.Kind == yaml.ScalarNode {
		b := boolValue(v.Value)
		out.AllowPrivilegeEscalation = &b
	}
	if caps := findChild(ctx, "capabilities"); caps != nil && caps.Kind == yaml.MappingNode {
		out.CapabilitiesAdd = readStringList(findChild(caps, "add"))
		out.CapabilitiesDrop = readStringList(findChild(caps, "drop"))
	}
}

func readStringList(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode && c.Value != "" {
			out = append(out, c.Value)
		}
	}
	return out
}

func parseInt64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var negative bool
	i := 0
	if s[0] == '-' {
		negative = true
		i = 1
	}
	var v int64
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int64(c-'0')
	}
	if negative {
		v = -v
	}
	return v, true
}

func boolValue(s string) bool {
	switch s {
	case "true", "True", "TRUE", "yes", "Yes", "1":
		return true
	}
	return false
}
