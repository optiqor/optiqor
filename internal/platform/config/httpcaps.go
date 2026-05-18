package config

// HTTP request body-size caps for every public ingress.
//
// Each ingress sets its own cap because the payloads differ in nature:
//
//   - GitHub webhooks carry large marketplace and push events. 8 MiB
//     covers the upper tail of legitimate deliveries.
//   - /v1/ingest is the agent uploading a 30-day Prometheus snapshot
//     for a medium tenant. 16 MiB matches the observed ceiling.
//   - /v1/analyze takes a Helm values.yaml. Files past 1 MiB are
//     pathological (a values.yaml is config, not data).
//   - /v1/apply-fixes takes a preview body the size of a few diffs.
//     1 MiB is comfortable.
//   - AWS Cost Anomaly Detection webhooks are tiny. 64 KiB is plenty.
//
// All five live in one file so a future bump is a single grep and the
// reasoning is colocated. Add new endpoints here, not as bare literals
// at the call site.
const (
	// GitHubWebhookMaxBytes caps inbound GitHub App webhook bodies.
	GitHubWebhookMaxBytes = 8 << 20 // 8 MiB

	// IngestMaxBytes caps the /v1/ingest payload from the in-cluster
	// agent (Prometheus snapshot + workload inventory).
	IngestMaxBytes = 16 << 20 // 16 MiB

	// SandboxAnalyzeMaxBytes caps /v1/analyze input (Helm values.yaml).
	SandboxAnalyzeMaxBytes = 1 << 20 // 1 MiB

	// PRApplyFixMaxBytes caps the /v1/apply-fixes preview body.
	PRApplyFixMaxBytes = 1 << 20 // 1 MiB

	// BillingSpikeWebhookMaxBytes caps AWS Cost Anomaly Detection
	// webhook deliveries.
	BillingSpikeWebhookMaxBytes = 64 << 10 // 64 KiB
)
