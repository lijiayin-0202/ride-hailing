package data

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStationSchemaAddsStatusAndDirection(t *testing.T) {
	pool := openModule3TestPool(t)
	assertStationColumn(t, pool, "status", "smallint", "1")
	assertStationColumn(t, pool, "service_direction", "smallint", "3")
	assertStationConstraint(t, pool, "stations_status_check")
	assertStationConstraint(t, pool, "stations_service_direction_check")
	assertStationConstraint(t, pool, "stations_service_area_name_key")
}

func TestApplyStationMigrationToIsolatedDatabase(t *testing.T) {
	pool := openModule3TestPool(t)
	if stationSchemaExists(t, pool) {
		t.Skip("station migration already applied to isolated database")
	}
	path := filepath.Join(filepath.Dir(filepath.Dir(findLocalTestEnvironment(t))),
		"migrations", "0003_station_status_and_direction.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read station migration: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply station migration to isolated database: %v", err)
	}
}

func stationSchemaExists(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.columns "+
			"WHERE table_schema = 'public' AND table_name = 'stations' "+
			"AND column_name = 'service_direction')",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check station schema: %v", err)
	}
	return exists
}

func assertStationColumn(t *testing.T, pool *pgxpool.Pool, column, dataType, defaultValue string) {
	t.Helper()
	var actualType, actualDefault string
	err := pool.QueryRow(context.Background(),
		"SELECT data_type, column_default\n"+
			"FROM information_schema.columns\n"+
			"WHERE table_schema = 'public' AND table_name = 'stations' AND column_name = $1", column,
	).Scan(&actualType, &actualDefault)
	if err != nil {
		t.Fatalf("read stations.%s metadata: %v", column, err)
	}
	if actualType != dataType || !strings.Contains(actualDefault, defaultValue) {
		t.Errorf("stations.%s = type:%q default:%q, want type:%q containing %q", column, actualType, actualDefault, dataType, defaultValue)
	}
}

func assertStationConstraint(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)", name,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("read %s constraint: %v", name, err)
	}
	if !exists {
		t.Errorf("constraint %q does not exist", name)
	}
}
