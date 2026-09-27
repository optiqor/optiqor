package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// SpikeNotifier delivers the notification (Slack DM, email, dashboard
// event). The workflow doesn't care which.
type SpikeNotifier interface {
	NotifySpike(ctx context.Context, t tenancy.Context, ev SpikeEvent) error
}

type SpikeEvent struct {
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAt        time.Time `json:"observed_at"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

type CostSpikePayload struct {
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAt        time.Time `json:"observed_at"`
	MinDeltaThreshold float64   `json:"min_delta_threshold"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

// CostSpike drops below-threshold events silently — the customer chose
// the floor when they enabled the feature.
type CostSpike struct {
	Notifier SpikeNotifier
}

func (CostSpike) Name() string { return "cost_spike" }

func (w CostSpike) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	if w.Notifier == nil {
		return fmt.Errorf("cost_spike: nil notifier")
	}
	var p CostSpikePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("cost_spike: decode: %w", err)
	}
	if p.ObservedDeltaUSD < p.MinDeltaThreshold {
		return nil
	}
	return w.Notifier.NotifySpike(ctx, t, SpikeEvent{
		WorkloadID:        p.WorkloadID,
		ObservedDeltaUSD:  p.ObservedDeltaUSD,
		ObservedAt:        p.ObservedAt,
		LikelyPRCommitSHA: p.LikelyPRCommitSHA,
		LikelyPRURL:       p.LikelyPRURL,
	})
}
