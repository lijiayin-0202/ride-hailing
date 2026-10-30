package data

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestDispatchRepositoryCreatePendingQueuesBothOutboxes(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	firstVehicleID, secondVehicleID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, firstVehicleID, secondVehicleID)
	setLatestVehiclePosition(t, ctx, pool, firstVehicleID, 116.397, 39.908)
	setLatestVehiclePosition(t, ctx, pool, secondVehicleID, 116.417, 39.908)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)

	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("NewSnowflakeDispatchIDGenerator() error = %v", err)
	}
	repository := NewDispatchRepository(&Data{pool: pool, dispatchIDGenerator: generator})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id = $1)", orderID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id = $1)", orderID)
	})
	result, err := repository.CreatePending(ctx, biz.PendingDispatchAttempt{
		Snapshot: biz.OrderDispatchSnapshot{OrderID: orderID, ServiceAreaID: areaID, Schedulable: true,
			Pickup:  biz.DispatchLocation{StationID: 11, Longitude: 116.407, Latitude: 39.908},
			Dropoff: biz.DispatchLocation{StationID: 12, Longitude: 116.417, Latitude: 39.918}},
		Deadline: time.Now().UTC().Add(30 * time.Second), Strategy: "first-eligible-v1", AverageSpeedKMH: 30,
	})
	if err != nil {
		t.Fatalf("CreatePending() error = %v", err)
	}
	if result.Record.Status != biz.DispatchStatusPending || result.Record.VehicleID != firstVehicleID {
		t.Fatalf("CreatePending() = %#v, want nearest pending dispatch", result)
	}
	assertAtomicOutboxRows(t, ctx, pool, result.Record.ID)
}

func TestDispatchRepositoryCreatePendingReusesActiveAttemptAfterUniqueConflict(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	firstVehicleID, secondVehicleID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, firstVehicleID, secondVehicleID)
	setLatestVehiclePosition(t, ctx, pool, firstVehicleID, 116.397, 39.908)
	setLatestVehiclePosition(t, ctx, pool, secondVehicleID, 116.417, 39.908)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)
	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("NewSnowflakeDispatchIDGenerator() error = %v", err)
	}
	repository := NewDispatchRepository(&Data{pool: pool, dispatchIDGenerator: generator})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id = $1)", orderID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id = $1)", orderID)
	})
	input := pendingAttemptInput(orderID, areaID)
	first, err := repository.CreatePending(ctx, input)
	if err != nil {
		t.Fatalf("first CreatePending() error = %v", err)
	}
	second, err := repository.CreatePending(ctx, input)
	if err != nil {
		t.Fatalf("second CreatePending() error = %v", err)
	}
	if !second.ReusedActiveAttempt || second.Record.ID != first.Record.ID {
		t.Fatalf("second CreatePending() = %#v, want reused first attempt %#v", second, first.Record)
	}
	if first.Record.CandidateScore <= 0 || second.Record.CandidateScore != 0.5397 || second.Record.EstimatedPickupDistanceKM != 0.85 || second.Record.EstimatedArrivalMin != 2 {
		t.Fatalf("reused dispatch metrics = %#v, want first dispatch metrics %#v", second.Record, first.Record)
	}
}

func TestDispatchRepositoryCreatePendingRollsBackWhenVehicleOutboxInsertFails(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	vehicleID, otherVehicleID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, vehicleID, otherVehicleID)
	setLatestVehiclePosition(t, ctx, pool, vehicleID, 116.397, 39.908)
	orderID, otherOrderID := base+20, base+21
	seedTripOrder(t, ctx, pool, orderID)
	seedTripOrder(t, ctx, pool, otherOrderID)
	const recordID int64 = 701
	const duplicateOutboxID int64 = 702
	repository := NewDispatchRepository(&Data{pool: pool, dispatchIDGenerator: &sequenceIDGenerator{values: []int64{recordID, duplicateOutboxID, 703}}})
	otherRecord := biz.DispatchRecord{ID: 704, DispatchNo: "DSP-rollback-seed", OrderID: otherOrderID, VehicleID: otherVehicleID, AttemptNo: 1, DispatchStrategy: "test", Status: biz.DispatchStatusCreated}
	if err := repository.Create(ctx, otherRecord); err != nil {
		t.Fatalf("create collision dispatch record: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.dispatch_outbox (id, dispatch_id, idempotency_key, payload, status)
		VALUES ($1, $2, $3, '{}'::jsonb, 0)`, duplicateOutboxID, otherRecord.ID, otherRecord.DispatchNo); err != nil {
		t.Fatalf("seed vehicle outbox collision: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.dispatch_outbox WHERE id = $1", duplicateOutboxID)
	})

	_, err := repository.CreatePending(ctx, pendingAttemptInput(orderID, areaID))
	if err == nil {
		t.Fatal("CreatePending() error = nil, want vehicle outbox primary key conflict")
	}
	assertNoPendingDispatchFacts(t, ctx, pool, orderID)
}

func pendingAttemptInput(orderID, areaID int64) biz.PendingDispatchAttempt {
	return biz.PendingDispatchAttempt{Snapshot: biz.OrderDispatchSnapshot{OrderID: orderID, ServiceAreaID: areaID, Schedulable: true,
		Pickup:  biz.DispatchLocation{StationID: 11, Longitude: 116.407, Latitude: 39.908},
		Dropoff: biz.DispatchLocation{StationID: 12, Longitude: 116.417, Latitude: 39.918}},
		Deadline: time.Now().UTC().Add(30 * time.Second), Strategy: "first-eligible-v1", AverageSpeedKMH: 30}
}

func assertNoPendingDispatchFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orderID int64) {
	t.Helper()
	for _, table := range []string{"dispatch_records", "order_dispatch_outbox"} {
		var count int
		query := "SELECT count(*) FROM public." + table + " WHERE order_id = $1"
		if table == "order_dispatch_outbox" {
			query = "SELECT count(*) FROM public.order_dispatch_outbox box JOIN public.dispatch_records record ON record.id = box.dispatch_id WHERE record.order_id = $1"
		}
		if err := pool.QueryRow(ctx, query, orderID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows for failed attempt = %d, want 0", table, count)
		}
	}
}

type sequenceIDGenerator struct {
	values []int64
	index  int
}

func (generator *sequenceIDGenerator) New() int64 {
	value := generator.values[generator.index]
	generator.index++
	return value
}

func setLatestVehiclePosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vehicleID int64, longitude, latitude float64) {
	t.Helper()
	_, err := pool.Exec(ctx, `UPDATE public.vehicle_status_logs
		SET current_longitude = $2, current_latitude = $3, reported_at = CURRENT_TIMESTAMP
		WHERE vehicle_id = $1`, vehicleID, longitude, latitude)
	if err != nil {
		t.Fatalf("set vehicle position: %v", err)
	}
}

func assertAtomicOutboxRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dispatchID int64) {
	t.Helper()
	for _, table := range []string{"dispatch_outbox", "order_dispatch_outbox"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM public."+table+" WHERE dispatch_id = $1", dispatchID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s rows = %d, want 1", table, count)
		}
	}
}
