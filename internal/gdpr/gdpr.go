// Package gdpr implements the legal-baseline obligations Sevro takes
// on for any EU customer:
//
//   - Data Subject Access Requests (DSAR): export and erase
//   - Data retention policies enforced server-side
//   - Subprocessor manifest helpers
//
// Phase 1 ships the contracts and the retention-policy table; the
// concrete export-ZIP builder + Temporal cron purge land alongside
// real Postgres wiring in Phase 5.
package gdpr

import (
	"context"
	"errors"
	"time"

	"github.com/lowplane/backend/internal/tenancy"
)

// Retention windows are committed in todo.md production-readiness
// gap #5 and the business strategy amendments. They live as constants
// here so cron jobs and audit reports read the same values.
const (
	// Prometheus snapshots ingested via the in-cluster agent.
	RetentionPrometheus = 90 * 24 * time.Hour
	// LLM call audit log (input/output hashes + cost).
	RetentionLLMCalls = 30 * 24 * time.Hour
	// Verified Receipts. 7 years per financial-records best practice.
	RetentionReceipts = 7 * 365 * 24 * time.Hour
	// Tenant-scoped audit log (every Apply Fix opened, dismissed, etc).
	RetentionAuditLog = 7 * 365 * 24 * time.Hour
	// DSAR erasure purge window. After this many days from the erase
	// request, tombstone records are physically removed.
	ErasurePurgeWindow = 30 * 24 * time.Hour
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

// UnknownBucketError is returned when a retention lookup names a
// bucket the policy table does not know about. Caller should treat
// this as a programming error (the bucket list is closed).
type UnknownBucketError struct {
	Bucket string
}

func (e *UnknownBucketError) Error() string {
	return "gdpr: unknown retention bucket " + e.Bucket
}

// Buckets returns the list of supported retention buckets in
// deterministic order. Used by the daily Temporal cron and by audit
// report generators.
func Buckets() []string {
	return []string{"prometheus", "llm_calls", "receipts", "audit_log"}
}

// ExportRequest carries the inputs to a DSAR export run.
type ExportRequest struct {
	Tenant    tenancy.Context
	Requested time.Time
	Format    ExportFormat
}

// ExportFormat is the on-disk layout returned to the data subject.
type ExportFormat string

const (
	// ExportZipJSON: a ZIP with one JSON file per tenant-scoped table.
	ExportZipJSON ExportFormat = "zip-json"
)

// ExportResult points to the produced export artefact and the
// cryptographic provenance the caller serves on the verification page.
type ExportResult struct {
	URL          string    // S3 presigned URL or local file path
	SizeBytes    int64
	SHA256       []byte
	GeneratedAt  time.Time
	GeneratedBy  string // backend version that produced the export
}

// EraseRequest carries the inputs to a DSAR erase run.
type EraseRequest struct {
	Tenant    tenancy.Context
	Requested time.Time
	// Reason is recorded in the audit log alongside the tombstone.
	Reason string
}

// EraseResult records what was scheduled for deletion.
type EraseResult struct {
	TombstoneID    string
	PurgeAfter     time.Time
	RecordsAffected int64
}

// Service is the contract `cmd/api` mounts against `/api/v1/dsar/...`.
// The Phase 1 implementation is unimplemented; it lives here so the
// route table compiles and integration tests can be written against
// the interface today.
type Service interface {
	Export(ctx context.Context, req ExportRequest) (ExportResult, error)
	Erase(ctx context.Context, req EraseRequest) (EraseResult, error)
}

// ErrNotImplemented is returned by stub Service methods until Phase 5
// wires the real Postgres + S3 path.
var ErrNotImplemented = errors.New("gdpr: not implemented in this phase")

// NoopService returns a Service whose methods all return
// ErrNotImplemented. Useful for testing the route table and as a safe
// default when the real service isn't configured.
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
