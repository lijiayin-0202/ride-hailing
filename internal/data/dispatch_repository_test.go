package data

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var nextDispatchTestStatusID int64

func TestDispatchRepositoryListEligibleUsesLatestVehicleStatus(t *testing.T) {
	pool := openModule3TestPool(t)
	context := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	firstID, secondID := base+3, base+4
	seedDispatchCandidateData(t, context, pool, areaID, otherAreaID, firstID, secondID)

	repository := NewDispatchRepository(&Data{pool: pool})
	candidates, err := repository.ListEligible(context, areaID, nil)
	if err != nil {
		t.Fatalf("ListEligible() error = %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("eligible candidate count = %d, want 2", len(candidates))
	}
	if candidates[0].ID != firstID || candidates[1].ID != secondID {
		t.Errorf("eligible vehicle IDs = [%d, %d], want [%d, %d]", candidates[0].ID, candidates[1].ID, firstID, secondID)
	}
}

func TestDispatchRepositoryListEligibleExcludesAttemptedVehicle(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	firstID, secondID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, firstID, secondID)

	repository := NewDispatchRepository(&Data{pool: pool})
	candidates, err := repository.ListEligible(ctx, areaID, []int64{firstID})
	if err != nil {
		t.Fatalf("ListEligible() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != secondID {
		t.Errorf("eligible candidates = %#v, want only vehicle %d", candidates, secondID)
	}
}

func TestDispatchRepositoryListEligibleRejectsEveryNonIdleLatestState(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	firstID, secondID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, firstID, secondID)
	seedAdditionalIneligibleVehicles(t, ctx, pool, areaID, firstID+7)

	candidates, err := NewDispatchRepository(&Data{pool: pool}).ListEligible(ctx, areaID, nil)
	if err != nil {
		t.Fatalf("ListEligible() error = %v", err)
	}
	if len(candidates) != 2 || candidates[0].ID != firstID || candidates[1].ID != secondID {
		t.Errorf("eligible candidates = %#v, want only healthy idle vehicles", candidates)
	}
}

func TestDispatchRepositoryCreatePersistsCreatedAttempt(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	vehicleID, secondID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, vehicleID, secondID)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)

	record := testDispatchRecord(base+30, orderID, vehicleID, 1)
	repository := NewDispatchRepository(&Data{pool: pool})
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	assertCreatedDispatchRecord(t, ctx, pool, record)
}

func TestDispatchRepositoryCreateMapsUniqueConflict(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	vehicleID, secondID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, vehicleID, secondID)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)

	repository := NewDispatchRepository(&Data{pool: pool})
	if err := repository.Create(ctx, testDispatchRecord(base+30, orderID, vehicleID, 1)); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	err := repository.Create(ctx, testDispatchRecord(base+31, orderID, vehicleID, 1))
	if !errors.Is(err, ErrDispatchAttemptConflict) {
		t.Errorf("second Create() error = %v, want ErrDispatchAttemptConflict", err)
	}
}

func seedTripOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orderID int64) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.dispatch_records WHERE order_id = $1", orderID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.trip_orders WHERE id = $1", orderID)
	})
	if _, err := pool.Exec(ctx, "INSERT INTO public.trip_orders (id) VALUES ($1)", orderID); err != nil {
		t.Fatalf("insert trip order: %v", err)
	}
}

func testDispatchRecord(id, orderID, vehicleID int64, attemptNo int16) biz.DispatchRecord {
	return biz.DispatchRecord{
		ID:               -id,
		DispatchNo:       fmt.Sprintf("dispatch-%x", -id),
		OrderID:          orderID,
		VehicleID:        vehicleID,
		AttemptNo:        attemptNo,
		DispatchStrategy: "test-strategy",
		Status:           biz.DispatchStatusCreated,
	}
}

func assertCreatedDispatchRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, record biz.DispatchRecord) {
	t.Helper()
	var status int16
	var dispatchedAt *time.Time
	var strategy string
	err := pool.QueryRow(ctx, `SELECT status, dispatched_at, dispatch_strategy
		FROM public.dispatch_records WHERE id = $1`, record.ID).Scan(&status, &dispatchedAt, &strategy)
	if err != nil {
		t.Fatalf("read created dispatch record: %v", err)
	}
	if status != biz.DispatchStatusCreated || dispatchedAt != nil || strategy != record.DispatchStrategy {
		t.Errorf("created record = status:%d dispatchedAt:%v strategy:%q", status, dispatchedAt, strategy)
	}
}

func seedDispatchCandidateData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID, otherAreaID, firstID, secondID int64) {
	t.Helper()
	vehicleIDs := []int64{firstID, secondID, firstID + 2, firstID + 3, firstID + 4, firstID + 5, firstID + 6}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicle_status_logs WHERE vehicle_id = ANY($1)", vehicleIDs)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicles WHERE id = ANY($1)", vehicleIDs)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = ANY($1)", []int64{areaID, otherAreaID})
	})
	insertServiceArea(t, ctx, pool, areaID, fmt.Sprint(-areaID))
	insertServiceArea(t, ctx, pool, otherAreaID, fmt.Sprint(-otherAreaID))

	now := time.Now().UTC()
	seedVehicleWithStatus(t, ctx, pool, firstID, areaID, 1, 1, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, secondID, areaID, 1, 1, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+2, areaID, 1, 1, true, nil, now.Add(-time.Minute))
	seedVehicleWithStatus(t, ctx, pool, firstID+2, areaID, 0, 1, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+3, areaID, 1, 2, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+4, areaID, 1, 1, false, nil, now)
	fault := "E-001"
	seedVehicleWithStatus(t, ctx, pool, firstID+5, areaID, 1, 1, true, &fault, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+6, otherAreaID, 1, 1, true, nil, now)
}

func seedAdditionalIneligibleVehicles(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID, firstID int64) {
	t.Helper()
	vehicleIDs := []int64{firstID, firstID + 1, firstID + 2, firstID + 3, firstID + 4, firstID + 5}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicle_status_logs WHERE vehicle_id = ANY($1)", vehicleIDs)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicles WHERE id = ANY($1)", vehicleIDs)
	})
	now := time.Now().UTC()
	seedVehicleWithStatus(t, ctx, pool, firstID, areaID, 1, 0, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+1, areaID, 1, 3, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+2, areaID, 1, 4, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+3, areaID, 1, 5, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+4, areaID, 1, 1, true, nil, now)
	seedVehicleWithStatus(t, ctx, pool, firstID+4, areaID, 1, 5, true, nil, now)
	if err := ensureVehicle(t, ctx, pool, firstID+5, areaID); err != nil {
		t.Fatal(err)
	}
}

func insertServiceArea(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, code string) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO public.service_areas
		(id, area_code, area_name, city_code, city_name, boundary_geojson, status)
		VALUES ($1, $2, $3, 'test-city', '测试城市', '{}'::jsonb, 1)`, id, code, code)
	if err != nil {
		t.Fatalf("insert service area: %v", err)
	}
}

func seedVehicleWithStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vehicleID, areaID int64, onlineStatus, operationStatus int16, accepts bool, fault *string, reportedAt time.Time) {
	t.Helper()
	if err := ensureVehicle(t, ctx, pool, vehicleID, areaID); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO public.vehicle_status_logs
		(id, vehicle_id, online_status, operation_status, fault_code, is_accepting_dispatch, reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, atomic.AddInt64(&nextDispatchTestStatusID, 1), vehicleID, onlineStatus, operationStatus, fault, accepts, reportedAt)
	if err != nil {
		t.Fatalf("insert vehicle status: %v", err)
	}
}

func ensureVehicle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vehicleID, areaID int64) error {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO public.vehicles
		(id, vehicle_no, vin, plate_no, vehicle_model, seat_count, service_area_id, operation_status, online_status)
		VALUES ($1, $2, $3, $4, 'test-model', 4, $5, 1, 1)
		ON CONFLICT (id) DO NOTHING`, vehicleID, fmt.Sprint("vehicle-", -vehicleID), fmt.Sprint("vin-", -vehicleID), fmt.Sprintf("P%x", -vehicleID), areaID)
	return err
}

func openModule3TestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	environmentPath := findLocalTestEnvironment(t)
	values := readEnvironmentFile(t, environmentPath)
	connectionURL := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(values["POSTGRES_USER"], values["POSTGRES_PASSWORD"]),
		Host:     "127.0.0.1:55432",
		Path:     values["POSTGRES_DB"],
		RawQuery: "sslmode=disable",
	}).String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, connectionURL)
	if err != nil {
		t.Fatalf("connect isolated module3 database: %v", err)
	}
	t.Cleanup(pool.Close)
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || database != "module3_test" {
		t.Fatalf("expected isolated module3_test database, got %q (error: %v)", database, err)
	}
	return pool
}

func findLocalTestEnvironment(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(directory, ".local", "module3-test-db.env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("isolated module3 test database environment file is missing")
		}
		directory = parent
	}
}

func readEnvironmentFile(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open isolated database environment file: %v", err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found {
			values[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read isolated database environment file: %v", err)
	}
	if values["POSTGRES_DB"] != "module3_test" || values["POSTGRES_USER"] != "module3_test" || values["POSTGRES_PASSWORD"] == "" {
		t.Fatal("isolated database environment is incomplete or targets an unexpected database")
	}
	return values
}
