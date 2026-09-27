//go:build integration

// Package integration boots real Postgres + Redis via testcontainers
// and applies the production migrations. Build tag keeps it out of
// the unit suite.
package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

type Harness struct {
	PG    *pgxpool.Pool
	Redis *redis.Client

	pgC    *pgcontainer.PostgresContainer
	rdC    *rediscontainer.RedisContainer
	pgConn string
	rdConn string
}

func New(t *testing.T) *Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pgC, err := pgcontainer.Run(ctx,
		"postgres:16-alpine",
		pgcontainer.WithDatabase("optiqor"),
		pgcontainer.WithUsername("optiqor_migrator"),
		pgcontainer.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("postgres container: %v", err)
	}
	pgConn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connstr: %v", err)
	}

	rdC, err := rediscontainer.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	rdConn, err := rdC.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis connstr: %v", err)
	}

	pool, err := pgxpool.New(ctx, pgConn)
	if err != nil {
		t.Fatalf("pgx pool: %v", err)
	}

	rdOpts, err := redis.ParseURL(rdConn)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	rdc := redis.NewClient(rdOpts)
	if err := rdc.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}

	h := &Harness{PG: pool, Redis: rdc, pgC: pgC, rdC: rdC, pgConn: pgConn, rdConn: rdConn}

	if err := h.applyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = rdc.Close()
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = pgC.Terminate(ctx)
		_ = rdC.Terminate(ctx)
	})
	return h
}

func (h *Harness) PGConn() string { return h.pgConn }

// AppPool acquires connections under optiqor_app (RLS-subject); the
// default h.PG pool runs as optiqor_migrator (BYPASSRLS).
func (h *Harness) AppPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(h.pgConn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE optiqor_app")
		return err
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("app pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func (h *Harness) applyMigrations(ctx context.Context) error {
	dir, err := migrationsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // dir is repo-pinned, name is a directory entry
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		sql := extractUp(string(body))
		if sql == "" {
			return fmt.Errorf("%s: no -- +goose Up block", name)
		}
		if _, err := h.PG.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

func migrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("integration: cannot resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", "migrations")), nil
}

// extractUp pulls the Up SQL out of a goose-style migration; we don't
// link goose itself just to parse its `-- +goose Up/Down` frames.
func extractUp(body string) string {
	const upTag = "-- +goose Up"
	const downTag = "-- +goose Down"
	const stmtBegin = "-- +goose StatementBegin"
	const stmtEnd = "-- +goose StatementEnd"

	up := strings.Index(body, upTag)
	if up < 0 {
		return ""
	}
	rest := body[up+len(upTag):]
	if d := strings.Index(rest, downTag); d >= 0 {
		rest = rest[:d]
	}
	rest = strings.ReplaceAll(rest, stmtBegin, "")
	rest = strings.ReplaceAll(rest, stmtEnd, "")
	return strings.TrimSpace(rest)
}

// ErrNoBinder is returned by BindTenant if the tenant context is empty.
var ErrNoBinder = errors.New("integration: empty tenant id")
