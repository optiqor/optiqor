package rules

import (
	"fmt"

	"github.com/optiqor/optiqor-cli/pkg/parser"
)

// excessiveReplicaCount fires when a workload declares more than 20
// replicas — even when an HPA is configured. Twenty is well past the
// point where additional replicas stop adding HA value (most clouds
// can't fit them across enough fault domains). Above this threshold
// the cost grows linearly while the marginal availability gain
// approaches zero.
//
// `replicas-too-high` (no HPA + replicas > 5) covers the easier case;
// this detector covers the case where the HPA is configured but the
// upper bound itself is unreasonable.
type excessiveReplicaCount struct{}

func newExcessiveReplicaCount() Detector { return excessiveReplicaCount{} }

func (excessiveReplicaCount) ID() string   { return "excessive-replica-count" }
func (excessiveReplicaCount) Name() string { return "Excessive replica count" }

const excessiveReplicaThreshold = 20

func (excessiveReplicaCount) Run(w parser.Workload) []Finding {
	if w.Replicas <= excessiveReplicaThreshold {
		return nil
	}
	return []Finding{{
		DetectorID: "excessive-replica-count",
		Workload:   w.Name,
		Title:      "Replica count past the HA inflection point",
		Detail:     fmt.Sprintf("Replicas set to %d. Past ~20 replicas the cost grows linearly while the marginal availability gain approaches zero — most cloud zones can't fit that many across enough fault domains. Cap the HPA's maxReplicas or split the workload across multiple deployments.", w.Replicas),
		Severity:   SeverityMed,
		Confidence: ConfidenceMed,
		Signal: &Signal{
			Label:       "replicas",
			Have:        float64(excessiveReplicaThreshold),
			Want:        float64(w.Replicas),
			HaveDisplay: fmt.Sprintf("%d", excessiveReplicaThreshold),
			WantDisplay: fmt.Sprintf("%d", w.Replicas),
			Note:        fmt.Sprintf("%d replicas", w.Replicas),
		},
	}}
}
