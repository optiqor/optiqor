// Package gdpr is the EU legal-baseline surface: DSAR export/erase,
// server-side retention policy, subprocessor manifest helpers. Phase 1
// is the contracts + retention table; the export ZIP builder and
// Temporal cron purge land in Phase 5.
package gdpr

import (
	"context"
	"errors"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Retention windows are committed per todo.md production-readiness
// gap #5 and the business-strategy amendments. Cron jobs and audit
// reports read the same constants.
//
//	prometheus  — 90d  (agent-ingested snapshots)
//	llm_calls   — 30d  (input/output hashes + cost)
//	receipts    — 7y   (financial-records best practice)
//	audit_log   — 7y   (Apply Fix lifecycle events)
//	erasure     — 30d  (purge window after a DSAR erase request)
const (
	RetentionPrometheus = 90 * 24 * time.Hour
	RetentionLLMCalls   = 30 * 24 * time.Hour
	RetentionReceipts   = 7 * 365 * 24 * time.Hour
	RetentionAuditLog   = 7 * 365 * 24 * time.Hour
	ErasurePurgeWindow  = 30 * 24 * time.Hour
)

// RetentionPolicy returns the cutoff time before which rows in the
// named bucket are eligible for deletion.
func RetentionPolicy(bucket string, now time.Time) (time.Time, error) {
	switch bucket {
	case "prometheus":
		return now.Add(-RetentionPrometheus), nil
	case "llm_calls":
		return now.Add(-RetentionLLMCalls), nil
	case "receipts":
		return now.Add(-RetentionReceipts), nil
	case "audit_log":
		return now.Add(-RetentionAuditLog), nil
	default:
		return time.Time{}, &UnknownBucketError{Bucket: bucket}
	}
}

// UnknownBucketError signals a closed-list mismatch — treat as a
// programming error.
type UnknownBucketError struct {
	Bucket string
}

func (e *UnknownBucketError) Error() string {
	return "gdpr: unknown retention bucket " + e.Bucket
}

// Buckets is the deterministic supported list. Daily Temporal cron and
// audit reports iterate it.
func Buckets() []string {
	return []string{"prometheus", "llm_calls", "receipts", "audit_log"}
}

type ExportRequest struct {
	Tenant    tenancy.Context
	Requested time.Time
	Format    ExportFormat
}

type ExportFormat string

const (
	// ExportZipJSON: ZIP with one JSON file per tenant-scoped table.
	ExportZipJSON ExportFormat = "zip-json"
)

type ExportResult struct {
	URL         string // S3 presigned URL or local file path
	SizeBytes   int64
	SHA256      []byte
	GeneratedAt time.Time
	GeneratedBy string // backend version that produced the export
}

type EraseRequest struct {
	Tenant    tenancy.Context
	Requested time.Time
	Reason    string // recorded on the tombstone audit row
}

type EraseResult struct {
	TombstoneID     string
	PurgeAfter      time.Time
	RecordsAffected int64
}

// Service is what cmd/api mounts at /api/v1/dsar/... . Phase 1 ships
// the interface only; Phase 5 wires the Postgres + S3 path.
type Service interface {
	Export(ctx context.Context, req ExportRequest) (ExportResult, error)
	Erase(ctx context.Context, req EraseRequest) (EraseResult, error)
}

var ErrNotImplemented = errors.New("gdpr: not implemented in this phase")

// NoopService is the safe default when the real service isn't
// configured; it still validates the tenant scope.
func NoopService() Service { return noopService{} }

type noopService struct{}

func (noopService) Export(_ context.Context, req ExportRequest) (ExportResult, error) {
	if err := req.Tenant.Validate(); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{}, ErrNotImplemented
}

func (noopService) Erase(_ context.Context, req EraseRequest) (EraseResult, error) {
	if err := req.Tenant.Validate(); err != nil {
		return EraseResult{}, err
	}
	return EraseResult{}, ErrNotImplemented
}
