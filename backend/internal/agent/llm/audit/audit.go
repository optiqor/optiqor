// Package audit records the forensic trail for every LLM call.
// Different from BudgetRecorder, which writes to llm_calls for cost
// attribution: this one records input + output hashes so a downstream
// investigation can answer "did the LLM hallucinate on this input?"
// without re-running the prompt.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type Record struct {
	ID           string
	Workload     string
	Model        string
	InputSHA256  string
	OutputSHA256 string
	InputBytes   int
	OutputBytes  int
	CostUSDCents int64
	Suspicious   bool
	At           time.Time
}

type Auditor interface {
	Record(ctx context.Context, t tenancy.Context, r Record) error
}

// Hash returns the lowercase hex SHA-256 of s; used at every call site
// so producers can't accidentally store plaintext prompts.
func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// InMemoryAuditor is goroutine-safe; tests and dev use it. Production
// swaps a Postgres-backed Auditor in behind the same interface.
type InMemoryAuditor struct {
	mu      sync.RWMutex
	records []Record
}

func NewInMemoryAuditor() *InMemoryAuditor {
	return &InMemoryAuditor{}
}

func (a *InMemoryAuditor) Record(_ context.Context, t tenancy.Context, r Record) error {
	if t.TenantID == "" {
		return errors.New("audit: empty tenant id")
	}
	if r.InputSHA256 == "" || r.OutputSHA256 == "" {
		return errors.New("audit: missing input or output hash")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, r)
	return nil
}

func (a *InMemoryAuditor) Len() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.records)
}

func (a *InMemoryAuditor) Snapshot() []Record {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Record, len(a.records))
	copy(out, a.records)
	return out
}

// NullAuditor satisfies Auditor and drops every record. Useful when
// audit recording is gated behind a feature flag and the call site
// shouldn't have to nil-check.
type NullAuditor struct{}

func (NullAuditor) Record(_ context.Context, _ tenancy.Context, _ Record) error { return nil }
