package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyDispatchOutboxMigrationToIsolatedDatabase(t *testing.T) {
	pool := openModule3TestPool(t)
	if dispatchOutboxTableExists(t, context.Background(), pool) {
		t.Skip("dispatch outbox migration already applied to isolated database")
	}
	path := filepath.Join(filepath.Dir(filepath.Dir(findLocalTestEnvironment(t))),
		"migrations", "0004_dispatch_outbox.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dispatch outbox migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply dispatch outbox migration to isolated database: %v", err)
	}
}

func TestDispatchOutboxSchemaSupportsReliableDelivery(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()

	assertDispatchOutboxTable(t, ctx, pool)
	assertDispatchOutboxConstraint(t, ctx, pool, "dispatch_outbox_status_check")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_dispatch_outbox_ready")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_dispatch_outbox_lease")
}

func assertDispatchOutboxTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if !dispatchOutboxTableExists(t, ctx, pool) {
		t.Fatal("dispatch_outbox table does not exist")
	}
}

func dispatchOutboxTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'dispatch_outbox'
	)`).Scan(&exists)
	if err != nil {
		t.Fatalf("read dispatch_outbox table metadata: %v", err)
	}
	return exists
}

func assertDispatchOutboxConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)", name).Scan(&exists)
	if err != nil {
		t.Fatalf("read outbox constraint %q: %v", name, err)
	}
	if !exists {
		t.Errorf("outbox constraint %q does not exist", name)
	}
}

func assertDispatchOutboxIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = $1
	)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("read outbox index %q: %v", name, err)
	}
	if !exists {
		t.Errorf("outbox index %q does not exist", name)
	}
}
