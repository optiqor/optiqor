package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/backend/internal/tenancy"
)

// SpikeNotifier delivers the spike notification (Slack DM, email,
// dashboard event). The workflow doesn't care which.
type SpikeNotifier interface {
	NotifySpike(ctx context.Context, t tenancy.Context, ev SpikeEvent) error
}

// SpikeEvent is the human-readable summary of an anomaly.
type SpikeEvent struct {
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAt        time.Time `json:"observed_at"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

// CostSpikePayload is the dispatcher input.
type CostSpikePayload struct {
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAt        time.Time `json:"observed_at"`
	MinDeltaThreshold float64   `json:"min_delta_threshold"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

// CostSpike threshold-filters anomalies and dispatches a notification.
// Below-threshold events drop silently — the customer chose the
// floor when they enabled the feature.
type CostSpike struct {
	Notifier SpikeNotifier
}

// Name is the dispatcher key.
func (CostSpike) Name() string { return "cost_spike" }

// Execute notifies on above-threshold events. Threshold defaults to
// $0 (always notify) if the caller didn't set one.
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
