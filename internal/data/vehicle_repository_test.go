package data

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var nextVehicleSnapshotStatusID = -time.Now().UnixNano()

func TestVehicleRepositoryListSnapshotsUsesLatestStatus(t *testing.T) {
	pool, ctx, areaID, vehicleID := setupVehicleSnapshotTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	insertVehicleSnapshotStatus(t, ctx, pool, vehicleID, 0, 2, false, "old", now.Add(-time.Minute))
	insertVehicleSnapshotStatus(t, ctx, pool, vehicleID, 1, 1, true, "", now)

	snapshots, err := NewVehicleRepository(&Data{pool: pool}).ListSnapshots(ctx, areaID)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	assertVehicleSnapshotStatus(t, snapshots, vehicleID, biz.VehicleOnline, biz.VehicleOperationIdle, true, "")
	assertVehicleSnapshotFields(t, snapshots[0], vehicleID, now)
}

func TestVehicleRepositoryListSnapshotsUsesIDForReportedAtTie(t *testing.T) {
	pool, ctx, areaID, vehicleID := setupVehicleSnapshotTest(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	insertVehicleSnapshotStatus(t, ctx, pool, vehicleID, 1, 2, true, "", now)
	insertVehicleSnapshotStatus(t, ctx, pool, vehicleID, 1, 1, true, "", now)

	snapshots, err := NewVehicleRepository(&Data{pool: pool}).ListSnapshots(ctx, areaID)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	assertVehicleSnapshotStatus(t, snapshots, vehicleID, biz.VehicleOnline, biz.VehicleOperationIdle, true, "")
}

func TestVehicleRepositoryListSnapshotsKeepsVehicleWithoutStatus(t *testing.T) {
	pool, ctx, areaID, vehicleID := setupVehicleSnapshotTest(t)

	snapshots, err := NewVehicleRepository(&Data{pool: pool}).ListSnapshots(ctx, areaID)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Vehicle.ID != vehicleID || snapshots[0].RuntimeStatus != nil {
		t.Errorf("ListSnapshots() = %#v, want vehicle without runtime status", snapshots)
	}
}

func TestVehicleRepositoryListSnapshotsFiltersServiceArea(t *testing.T) {
	pool, ctx, areaID, vehicleID := setupVehicleSnapshotTest(t)
	otherAreaID := areaID - 1
	insertServiceArea(t, ctx, pool, otherAreaID, fmt.Sprintf("snapshot-other-%d", -otherAreaID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", otherAreaID)
	})
	otherVehicleID := vehicleID - 1
	insertVehicleSnapshotVehicle(t, ctx, pool, otherVehicleID, otherAreaID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicle_status_logs WHERE vehicle_id = $1", otherVehicleID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicles WHERE id = $1", otherVehicleID)
	})

	snapshots, err := NewVehicleRepository(&Data{pool: pool}).ListSnapshots(ctx, areaID)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Vehicle.ID != vehicleID {
		t.Errorf("ListSnapshots() = %#v, want only vehicle %d", snapshots, vehicleID)
	}
}

func setupVehicleSnapshotTest(t *testing.T) (*pgxpool.Pool, context.Context, int64, int64) {
	t.Helper()
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, vehicleID := base, base-1
	insertServiceArea(t, ctx, pool, areaID, fmt.Sprintf("snapshot-area-%d", -areaID))
	insertVehicleSnapshotVehicle(t, ctx, pool, vehicleID, areaID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicle_status_logs WHERE vehicle_id = $1", vehicleID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicles WHERE id = $1", vehicleID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", areaID)
	})
	return pool, ctx, areaID, vehicleID
}

func insertVehicleSnapshotVehicle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vehicleID, areaID int64) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO public.vehicles
		(id, vehicle_no, vin, plate_no, vehicle_model, seat_count, service_area_id, operation_status, online_status, software_version)
		VALUES ($1, $2, $3, $4, 'snapshot-model', 4, $5, 0, 0, 'snapshot-v1')`,
		vehicleID, fmt.Sprintf("snapshot-no-%d", vehicleID), fmt.Sprintf("snapshot-vin-%d", vehicleID), fmt.Sprintf("S%x", -vehicleID), areaID)
	if err != nil {
		t.Fatalf("insert vehicle: %v", err)
	}
}

func insertVehicleSnapshotStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vehicleID int64, online, operation int16, accepts bool, fault string, reportedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO public.vehicle_status_logs
		(id, vehicle_id, online_status, operation_status, battery_percent, current_longitude, current_latitude, fault_code, is_accepting_dispatch, reported_at)
		VALUES ($1, $2, $3, $4, 80.5, 116.397128, 39.916527, $5, $6, $7)`,
		atomic.AddInt64(&nextVehicleSnapshotStatusID, 1), vehicleID, online, operation, fault, accepts, reportedAt)
	if err != nil {
		t.Fatalf("insert vehicle status: %v", err)
	}
}

func assertVehicleSnapshotStatus(t *testing.T, snapshots []biz.VehicleSnapshot, vehicleID int64, online, operation int16, accepts bool, fault string) {
	t.Helper()
	if len(snapshots) != 1 || snapshots[0].Vehicle.ID != vehicleID || snapshots[0].RuntimeStatus == nil {
		t.Fatalf("ListSnapshots() = %#v, want one vehicle with status", snapshots)
	}
	status := snapshots[0].RuntimeStatus
	if status.OnlineStatus != online || status.OperationStatus != operation || status.IsAcceptingDispatch != accepts || status.FaultCode != fault {
		t.Errorf("runtime status = %#v, want online:%d operation:%d accepts:%t fault:%q", status, online, operation, accepts, fault)
	}
}

func assertVehicleSnapshotFields(t *testing.T, snapshot biz.VehicleSnapshot, vehicleID int64, reportedAt time.Time) {
	t.Helper()
	vehicle := snapshot.Vehicle
	if vehicle.VehicleNo != fmt.Sprintf("snapshot-no-%d", vehicleID) || vehicle.VIN != fmt.Sprintf("snapshot-vin-%d", vehicleID) || vehicle.PlateNo != fmt.Sprintf("S%x", -vehicleID) || vehicle.SoftwareVer != "snapshot-v1" {
		t.Errorf("vehicle archive = %#v, want mapped fields", vehicle)
	}
	status := snapshot.RuntimeStatus
	if status.BatteryPercent == nil || *status.BatteryPercent != 80.5 || status.Longitude == nil || *status.Longitude != 116.397128 || status.Latitude == nil || *status.Latitude != 39.916527 || !status.ReportedAt.Equal(reportedAt) {
		t.Errorf("runtime details = %#v, want mapped battery, coordinate, and reported time", status)
	}
}
