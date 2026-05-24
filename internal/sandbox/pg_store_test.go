package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

type fakeExec struct {
	execs      []execCall
	rowResult  fakeRow
	rowErr     error
	execErr    error
	execErrFor string // when set, only Exec calls whose sql contains this substring fail
}

type execCall struct {
	sql  string
	args []any
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("fakeRow: dest/value length mismatch")
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = r.values[i].(string)
		case *[]byte:
			*d = r.values[i].([]byte)
		case *int:
			*d = r.values[i].(int)
		case *time.Time:
			*d = r.values[i].(time.Time)
		default:
			return errors.New("fakeRow: unsupported dest type")
		}
	}
	return nil
}

func (f *fakeExec) QueryRow(_ context.Context, sql string, args ...any) PgRow {
	f.execs = append(f.execs, execCall{sql: sql, args: args})
	if f.rowErr != nil {
		return fakeRow{err: f.rowErr}
	}
	return f.rowResult
}

func (f *fakeExec) Exec(_ context.Context, sql string, args ...any) error {
	f.execs = append(f.execs, execCall{sql: sql, args: args})
	if f.execErr != nil && (f.execErrFor == "" || strings.Contains(sql, f.execErrFor)) {
		return f.execErr
	}
	return nil
}

func TestPgStore_Put_HappyPath(t *testing.T) {
	exec := &fakeExec{}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := &PgStore{Exec: exec, Now: func() time.Time { return now }}

	sa := SharedAnalysis{
		Hash:      "abc123def456",
		Body:      []byte(`{"workloads":1}`),
		MediaType: "application/json",
		Source:    "sandbox",
		Workloads: 1,
		Findings:  []rules.Finding{{DetectorID: "cpu-overprovisioned"}},
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if err := s.Put(context.Background(), sa); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if len(exec.execs) != 1 {
		t.Fatalf("want 1 exec call, got %d", len(exec.execs))
	}
	got := exec.execs[0]
	if !strings.Contains(got.sql, "INSERT INTO shared_analyses") {
		t.Errorf("expected INSERT, got %q", got.sql)
	}
	if !strings.Contains(got.sql, "ON CONFLICT (hash)") {
		t.Errorf("expected upsert via ON CONFLICT, got %q", got.sql)
	}
	if got.args[0] != sa.Hash {
		t.Errorf("hash arg: got %v want %v", got.args[0], sa.Hash)
	}
	if got.args[2] != "sandbox" {
		t.Errorf("source arg: got %v want sandbox", got.args[2])
	}
}

func TestPgStore_Put_RejectsEmptyHash(t *testing.T) {
	s := &PgStore{Exec: &fakeExec{}}
	err := s.Put(context.Background(), SharedAnalysis{ExpiresAt: time.Now().Add(time.Hour), Source: "cli"})
	if err == nil || !strings.Contains(err.Error(), "empty hash") {
		t.Errorf("want empty-hash error, got %v", err)
	}
}

func TestPgStore_Put_RejectsInvalidSource(t *testing.T) {
	s := &PgStore{Exec: &fakeExec{}}
	err := s.Put(context.Background(), SharedAnalysis{
		Hash:      "h",
		Source:    "from-mars",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid source") {
		t.Errorf("want invalid-source error, got %v", err)
	}
}

func TestPgStore_Put_RejectsMissingExpiry(t *testing.T) {
	s := &PgStore{Exec: &fakeExec{}}
	err := s.Put(context.Background(), SharedAnalysis{Hash: "h", Source: "cli"})
	if err == nil || !strings.Contains(err.Error(), "expires_at") {
		t.Errorf("want missing-expires_at error, got %v", err)
	}
}

func TestPgStore_Put_DefaultsMediaType(t *testing.T) {
	exec := &fakeExec{}
	s := &PgStore{Exec: exec}
	sa := SharedAnalysis{
		Hash:      "h",
		Source:    "cli",
		Body:      []byte(`{}`),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.Put(context.Background(), sa); err != nil {
		t.Fatal(err)
	}
	got := exec.execs[0].args[3].(string)
	if got != "application/json" {
		t.Errorf("media_type default: got %q want application/json", got)
	}
}

func TestPgStore_Get_HappyPath(t *testing.T) {
	createdAt := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(30 * 24 * time.Hour)
	findings := []rules.Finding{{DetectorID: "cpu-overprovisioned"}}
	findingsJSON, _ := json.Marshal(findings)

	exec := &fakeExec{
		rowResult: fakeRow{values: []any{
			"sandbox",                 // source
			"application/json",        // media_type
			[]byte(`{"workloads":1}`), // payload
			1,                         // workloads
			findingsJSON,              // findings_json
			createdAt,                 // created_at
			expiresAt,                 // expires_at
		}},
	}
	s := &PgStore{Exec: exec, Now: func() time.Time { return createdAt }}

	sa, err := s.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sa.Hash != "abc" {
		t.Errorf("hash: got %q want abc", sa.Hash)
	}
	if sa.Source != "sandbox" {
		t.Errorf("source: got %q", sa.Source)
	}
	if string(sa.Body) != `{"workloads":1}` {
		t.Errorf("body: got %q", string(sa.Body))
	}
	if len(sa.Findings) != 1 || sa.Findings[0].DetectorID != "cpu-overprovisioned" {
		t.Errorf("findings round-trip: got %+v", sa.Findings)
	}
	if len(exec.execs) != 2 {
		t.Errorf("want 2 calls (read + view bump), got %d", len(exec.execs))
	}
	if !strings.Contains(exec.execs[1].sql, "view_count = view_count + 1") {
		t.Errorf("second call must bump view_count, got %q", exec.execs[1].sql)
	}
}

func TestPgStore_Get_EmptyHashReturnsNotFound(t *testing.T) {
	s := &PgStore{Exec: &fakeExec{}}
	_, err := s.Get(context.Background(), "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestPgStore_Get_NoRowsTranslatesToNotFound(t *testing.T) {
	exec := &fakeExec{rowErr: ErrPgNoRows}
	s := &PgStore{Exec: exec}
	_, err := s.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestPgStore_Get_ViewBumpFailureDoesNotPropagate(t *testing.T) {
	createdAt := time.Now().UTC()
	exec := &fakeExec{
		rowResult: fakeRow{values: []any{
			"cli", "application/json", []byte(`{}`),
			0, []byte(`[]`), createdAt, createdAt.Add(time.Hour),
		}},
		execErr:    errors.New("update failed"),
		execErrFor: "view_count",
	}
	s := &PgStore{Exec: exec}
	_, err := s.Get(context.Background(), "h")
	if err != nil {
		t.Errorf("a failed view_count bump must not surface to callers: %v", err)
	}
}
