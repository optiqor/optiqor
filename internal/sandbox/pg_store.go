package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// PgExec is the narrow Postgres surface PgStore needs. Driver-agnostic
// so pgx, database/sql, or a test fake can all satisfy it.
type PgExec interface {
	// QueryRow must return a Scan that reports ErrPgNoRows on an empty
	// result set; PgStore.Get checks for it via errors.Is.
	QueryRow(ctx context.Context, sql string, args ...any) PgRow
	Exec(ctx context.Context, sql string, args ...any) error
}

type PgRow interface {
	Scan(dest ...any) error
}

// ErrPgNoRows is the driver-agnostic "row missing" sentinel. Wrappers
// translate sql.ErrNoRows / pgx.ErrNoRows into this so PgStore stays
// driver-free.
var ErrPgNoRows = errors.New("sandbox/pg: no rows")

// PgStore is the production sandbox.Store over shared_analyses
// (migration 0004). Public by design: no RLS, no tenant binding — the
// share hash is the access token.
type PgStore struct {
	Exec PgExec
	// Now defaults to time.Now().UTC(); tests pin it.
	Now func() time.Time
}

func NewPgStore(e PgExec) *PgStore {
	return &PgStore{Exec: e}
}

// Put upserts on hash so a repeat POST of byte-identical canonical
// input collapses onto the same row, keeping share URLs stable across
// reanalysis. payload_sha256 stores the full digest (distinct from the
// 12-byte URL prefix in hash) so an audit can collision-check without
// re-reading payload bytes.
func (s *PgStore) Put(ctx context.Context, sa SharedAnalysis) error {
	if sa.Hash == "" {
		return errors.New("sandbox/pg: empty hash")
	}
	if sa.Source != "cli" && sa.Source != "sandbox" {
		return fmt.Errorf("sandbox/pg: invalid source %q", sa.Source)
	}
	if sa.ExpiresAt.IsZero() {
		return errors.New("sandbox/pg: missing expires_at")
	}

	mediaType := sa.MediaType
	if mediaType == "" {
		mediaType = "application/json"
	}

	findingsJSON, err := json.Marshal(sa.Findings)
	if err != nil {
		return fmt.Errorf("sandbox/pg: marshal findings: %w", err)
	}

	full := sha256.Sum256(sa.Body)

	const q = `
		INSERT INTO shared_analyses (
			hash, payload_sha256, source, media_type, payload,
			workloads, findings_json, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (hash) DO UPDATE SET
			expires_at = EXCLUDED.expires_at`
	return s.Exec.Exec(ctx, q,
		sa.Hash, full[:], sa.Source, mediaType, sa.Body,
		sa.Workloads, findingsJSON, sa.CreatedAt, sa.ExpiresAt,
	)
}

// Get returns ErrNotFound for both missing and expired rows; the SQL
// filters expires_at so a stale entry never leaks. The view_count bump
// is fire-and-forget — the read already succeeded.
func (s *PgStore) Get(ctx context.Context, hash string) (SharedAnalysis, error) {
	if hash == "" {
		return SharedAnalysis{}, ErrNotFound
	}

	const q = `
		SELECT source, media_type, payload, workloads, findings_json,
		       created_at, expires_at
		  FROM shared_analyses
		 WHERE hash = $1
		   AND expires_at > $2`

	now := s.now()
	row := s.Exec.QueryRow(ctx, q, hash, now)

	var (
		source      string
		mediaType   string
		payload     []byte
		workloads   int
		findingsRaw []byte
		createdAt   time.Time
		expiresAt   time.Time
	)
	if err := row.Scan(&source, &mediaType, &payload, &workloads, &findingsRaw, &createdAt, &expiresAt); err != nil {
		if errors.Is(err, ErrPgNoRows) {
			return SharedAnalysis{}, ErrNotFound
		}
		return SharedAnalysis{}, fmt.Errorf("sandbox/pg: scan: %w", err)
	}

	var findings []rules.Finding
	if len(findingsRaw) > 0 {
		if err := json.Unmarshal(findingsRaw, &findings); err != nil {
			return SharedAnalysis{}, fmt.Errorf("sandbox/pg: unmarshal findings: %w", err)
		}
	}

	_ = s.Exec.Exec(ctx, `UPDATE shared_analyses SET view_count = view_count + 1 WHERE hash = $1`, hash)

	return SharedAnalysis{
		Hash:      hash,
		Body:      payload,
		MediaType: mediaType,
		Source:    source,
		Workloads: workloads,
		Findings:  findings,
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *PgStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
