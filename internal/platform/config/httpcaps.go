package config

// Per-endpoint body-size caps. All caps live here so a future bump is
// one grep; never put a bare literal at the call site.
const (
	// 8 MiB covers the upper tail of legitimate GitHub webhook deliveries
	// (marketplace + push events).
	GitHubWebhookMaxBytes = 8 << 20

	// 16 MiB matches the observed ceiling of a 30-day Prometheus
	// snapshot from a medium tenant.
	IngestMaxBytes = 16 << 20

	// values.yaml is config, not data — past 1 MiB is pathological.
	SandboxAnalyzeMaxBytes = 1 << 20

	PRApplyFixMaxBytes = 1 << 20

	// AWS Cost Anomaly Detection webhooks are tiny.
	BillingSpikeWebhookMaxBytes = 64 << 10

	// /v1/session/issue is subject + tenant id + name — fits in one
	// TCP segment.
	SessionIssueMaxBytes = 16 << 10

	// /v1/onboarding/transition body is {"to": <stage>}.
	OnboardingTransitionMaxBytes = 4 << 10
)
