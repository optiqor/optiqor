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

func TestPgStore_Put(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name    string
		sa      SharedAnalysis
		wantErr string // substring; "" expects success. validation errors are unstructured strings, not sentinels.
		check   func(t *testing.T, exec *fakeExec)
	}{
		{
			name: "happy path",
			sa: SharedAnalysis{
				Hash:      "abc123def456",
				Body:      []byte(`{"workloads":1}`),
				MediaType: "application/json",
				Source:    "sandbox",
				Workloads: 1,
				Findings:  []rules.Finding{{DetectorID: "cpu-overprovisioned"}},
				CreatedAt: now,
				ExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			check: func(t *testing.T, exec *fakeExec) {
				t.Helper()
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
				if got.args[0] != "abc123def456" {
					t.Errorf("hash arg: got %v want abc123def456", got.args[0])
				}
				if got.args[2] != "sandbox" {
					t.Errorf("source arg: got %v want sandbox", got.args[2])
				}
			},
		},
		{
			name: "rejects empty hash",
			sa: SharedAnalysis{
				Source:    "cli",
				ExpiresAt: now.Add(time.Hour),
			},
			wantErr: "empty hash",
		},
		{
			name: "rejects invalid source",
			sa: SharedAnalysis{
				Hash:      "h",
				Source:    "from-mars",
				ExpiresAt: now.Add(time.Hour),
			},
			wantErr: "invalid source",
		},
		{
			name:    "rejects missing expiry",
			sa:      SharedAnalysis{Hash: "h", Source: "cli"},
			wantErr: "expires_at",
		},
		{
			name: "defaults media type",
			sa: SharedAnalysis{
				Hash:      "h",
				Source:    "cli",
				Body:      []byte(`{}`),
				ExpiresAt: now.Add(time.Hour),
			},
			check: func(t *testing.T, exec *fakeExec) {
				t.Helper()
				got := exec.execs[0].args[3].(string)
				if got != "application/json" {
					t.Errorf("media_type default: got %q want application/json", got)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := &fakeExec{}
			s := &PgStore{Exec: exec, Now: func() time.Time { return now }}
			err := s.Put(context.Background(), tc.sa)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.check != nil {
				tc.check(t, exec)
			}
		})
	}
}

func TestPgStore_Get(t *testing.T) {
	createdAt := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(30 * 24 * time.Hour)
	findings := []rules.Finding{{DetectorID: "cpu-overprovisioned"}}
	findingsJSON, _ := json.Marshal(findings)

	for _, tc := range []struct {
		name    string
		exec    *fakeExec
		hash    string
		wantErr error
		check   func(t *testing.T, sa SharedAnalysis, exec *fakeExec)
	}{
		{
			name: "happy path",
			exec: &fakeExec{
				rowResult: fakeRow{values: []any{
					"sandbox",
					"application/json",
					[]byte(`{"workloads":1}`),
					1,
					findingsJSON,
					createdAt,
					expiresAt,
				}},
			},
			hash: "abc",
			check: func(t *testing.T, sa SharedAnalysis, exec *fakeExec) {
				t.Helper()
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
			},
		},
		{
			name:    "empty hash returns not found",
			exec:    &fakeExec{},
			hash:    "",
			wantErr: ErrNotFound,
		},
		{
			name:    "no rows translates to not found",
			exec:    &fakeExec{rowErr: ErrPgNoRows},
			hash:    "missing",
			wantErr: ErrNotFound,
		},
		{
			// A failed view_count bump must not surface to callers: the
			// payload was already read, the metric is best-effort.
			name: "view bump failure does not propagate",
			exec: &fakeExec{
				rowResult: fakeRow{values: []any{
					"cli", "application/json", []byte(`{}`),
					0, []byte(`[]`), createdAt, expiresAt,
				}},
				execErr:    errors.New("update failed"),
				execErrFor: "view_count",
			},
			hash: "h",
		},
		{
			name: "expired row returns ErrExpired not ErrNotFound",
			exec: &fakeExec{
				rowResult: fakeRow{values: []any{
					"cli", "application/json", []byte(`{}`),
					0, []byte(`[]`), createdAt, createdAt.Add(time.Hour),
				}},
			},
			hash:    "stale",
			wantErr: ErrExpired,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &PgStore{Exec: tc.exec, Now: func() time.Time { return createdAt.Add(2 * time.Hour) }}
			sa, err := s.Get(context.Background(), tc.hash)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err: got %v want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.check != nil {
				tc.check(t, sa, tc.exec)
			}
		})
	}
}
