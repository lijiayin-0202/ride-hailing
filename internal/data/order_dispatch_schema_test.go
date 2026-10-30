package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyOrderDispatchMigrationToIsolatedDatabase(t *testing.T) {
	pool := openModule3TestPool(t)
	if orderDispatchOutboxTableExists(t, context.Background(), pool) {
		t.Skip("order dispatch migration already applied to isolated database")
	}
	path := filepath.Join(filepath.Dir(filepath.Dir(findLocalTestEnvironment(t))),
		"migrations", "0005_order_dispatch_sync_and_reassignment.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read order dispatch migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply order dispatch migration to isolated database: %v", err)
	}
}

func TestOrderDispatchMigrationCreatesRecoveryIndexes(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	if !orderDispatchOutboxTableExists(t, ctx, pool) || !tableExists(t, ctx, pool, "dispatch_reassignment_jobs") {
		t.Fatal("order dispatch migration tables do not exist")
	}
	assertDispatchOutboxConstraint(t, ctx, pool, "order_dispatch_outbox_dispatch_event_key")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_order_dispatch_outbox_ready")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_order_dispatch_outbox_lease")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_dispatch_reassignment_jobs_ready")
	assertDispatchOutboxIndex(t, ctx, pool, "idx_dispatch_reassignment_jobs_lease")
}

func orderDispatchOutboxTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool) bool {
	t.Helper()
	return tableExists(t, ctx, pool, "order_dispatch_outbox")
}

func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	)`, tableName).Scan(&exists)
	if err != nil {
		t.Fatalf("read table %q metadata: %v", tableName, err)
	}
	return exists
}
