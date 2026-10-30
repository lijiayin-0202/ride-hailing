package data

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestDispatchOutboxQueueAtomicallyMarksAttemptAndStoresSnapshot(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	queued, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction)
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if queued.Status != biz.DispatchStatusPending || queued.DispatchedAt == nil {
		t.Errorf("Queue() = %#v, want pending record with dispatched time", queued)
	}

	var payload []byte
	var status int16
	err = repository.data.pool.QueryRow(ctx, `SELECT payload, status
		FROM public.dispatch_outbox WHERE dispatch_id = $1`, instruction.Dispatch.ID).Scan(&payload, &status)
	if err != nil {
		t.Fatalf("read queued outbox: %v", err)
	}
	var stored biz.VehicleDispatchInstruction
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatalf("unmarshal queued payload: %v", err)
	}
	if status != 0 || stored.Dispatch.DispatchNo != instruction.Dispatch.DispatchNo || stored.Pickup.Address != "起点" || stored.Dropoff.Address != "终点" || !stored.ConfirmationDeadline.Equal(instruction.ConfirmationDeadline) {
		t.Errorf("stored outbox = status:%d payload:%#v", status, stored)
	}
}

func TestDispatchOutboxQueueRejectsSecondQueueWithoutDuplicatingSnapshot(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	queuer := dispatchOutboxQueuerOf(t, repository)
	if _, err := queuer.Queue(ctx, instruction); err != nil {
		t.Fatalf("first Queue() error = %v", err)
	}
	if _, err := queuer.Queue(ctx, instruction); err == nil {
		t.Fatal("second Queue() error = nil, want state conflict")
	}
	assertOutboxCount(t, ctx, repository, instruction.Dispatch.ID, 1)
}

func TestDispatchOutboxQueueRollsBackPendingStateWhenSnapshotCannotPersist(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	const conflictingOutboxID int64 = 9001
	seedOutboxPrimaryKeyCollision(t, ctx, repository, instruction, conflictingOutboxID)
	repository.data.dispatchIDGenerator = fixedOutboxIDGenerator{id: conflictingOutboxID}
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err == nil {
		t.Fatal("Queue() error = nil, want outbox persistence failure")
	}

	var status int16
	err := repository.data.pool.QueryRow(ctx, "SELECT status FROM public.dispatch_records WHERE id = $1", instruction.Dispatch.ID).Scan(&status)
	if err != nil {
		t.Fatalf("read dispatch status after queue failure: %v", err)
	}
	if status != biz.DispatchStatusCreated {
		t.Errorf("dispatch status after queue failure = %d, want %d", status, biz.DispatchStatusCreated)
	}
	assertOutboxCount(t, ctx, repository, instruction.Dispatch.ID, 0)
}

func TestDispatchOutboxQueueRejectsSnapshotWithDifferentDispatchFacts(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	instruction.Dispatch.VehicleID++
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err == nil {
		t.Fatal("Queue() error = nil, want inconsistent dispatch facts rejection")
	}

	var status int16
	err := repository.data.pool.QueryRow(ctx, "SELECT status FROM public.dispatch_records WHERE id = $1", instruction.Dispatch.ID).Scan(&status)
	if err != nil {
		t.Fatalf("read dispatch status after inconsistent snapshot: %v", err)
	}
	if status != biz.DispatchStatusCreated {
		t.Errorf("dispatch status after inconsistent snapshot = %d, want %d", status, biz.DispatchStatusCreated)
	}
	assertOutboxCount(t, ctx, repository, instruction.Dispatch.ID, 0)
}

func TestDispatchOutboxProcessorDeliversClaimedInstruction(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	processed, err := dispatchOutboxProcessorOf(t, repository).ProcessOutboxOnce(ctx, acceptingDispatchGateway{}, now, 10*time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOutboxOnce() = %t, %v; want processed delivery", processed, err)
	}

	var status int16
	var deliveredAt *time.Time
	err = repository.data.pool.QueryRow(ctx, "SELECT status, delivered_at FROM public.dispatch_outbox WHERE dispatch_id = $1", instruction.Dispatch.ID).Scan(&status, &deliveredAt)
	if err != nil {
		t.Fatalf("read delivered outbox: %v", err)
	}
	if status != 2 || deliveredAt == nil || !deliveredAt.Equal(now) {
		t.Errorf("outbox delivery = status:%d deliveredAt:%v, want delivered at %v", status, deliveredAt, now)
	}
}

func TestDispatchOutboxProcessorRetriesGatewayFailureWithBackoff(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	processed, err := dispatchOutboxProcessorOf(t, repository).ProcessOutboxOnce(ctx, failingDispatchGateway{}, now, 10*time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOutboxOnce() = %t, %v; want processed retry", processed, err)
	}

	var status, attempts int16
	var nextAttempt time.Time
	err = repository.data.pool.QueryRow(ctx, `SELECT status, attempt_count, next_attempt_at
		FROM public.dispatch_outbox WHERE dispatch_id = $1`, instruction.Dispatch.ID).Scan(&status, &attempts, &nextAttempt)
	if err != nil {
		t.Fatalf("read retried outbox: %v", err)
	}
	if status != 0 || attempts != 1 || !nextAttempt.Equal(now.Add(time.Second)) {
		t.Errorf("outbox retry = status:%d attempts:%d next:%v, want pending once at %v", status, attempts, nextAttempt, now.Add(time.Second))
	}
}

func TestDispatchOutboxProcessorRetriesRejectedDeliveryWithBackoff(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	processed, err := dispatchOutboxProcessorOf(t, repository).ProcessOutboxOnce(ctx, rejectingDispatchGateway{}, now, 10*time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOutboxOnce() = %t, %v; want processed retry", processed, err)
	}

	var status int16
	var attempts int
	err = repository.data.pool.QueryRow(ctx, `SELECT status, attempt_count
		FROM public.dispatch_outbox WHERE dispatch_id = $1`, instruction.Dispatch.ID).Scan(&status, &attempts)
	if err != nil {
		t.Fatalf("read rejected outbox: %v", err)
	}
	if status != 0 || attempts != 1 {
		t.Errorf("outbox after rejected delivery = status:%d attempts:%d, want pending once", status, attempts)
	}
}

func TestDispatchOutboxProcessorReclaimsExpiredLease(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := dispatchOutboxQueuerOf(t, repository).Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.dispatch_outbox
		SET status = 1, lease_expires_at = $2 WHERE dispatch_id = $1`, instruction.Dispatch.ID, now.Add(-time.Second)); err != nil {
		t.Fatalf("seed expired outbox lease: %v", err)
	}
	processed, err := dispatchOutboxProcessorOf(t, repository).ProcessOutboxOnce(ctx, acceptingDispatchGateway{}, now, 10*time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOutboxOnce() = %t, %v; want reclaimed delivery", processed, err)
	}

	var status int16
	err = repository.data.pool.QueryRow(ctx, "SELECT status FROM public.dispatch_outbox WHERE dispatch_id = $1", instruction.Dispatch.ID).Scan(&status)
	if err != nil {
		t.Fatalf("read reclaimed outbox: %v", err)
	}
	if status != 2 {
		t.Errorf("outbox status after lease reclaim = %d, want delivered", status)
	}
}

func seedOutboxPrimaryKeyCollision(t *testing.T, ctx context.Context, repository *DispatchRepository, instruction biz.VehicleDispatchInstruction, outboxID int64) {
	t.Helper()
	otherOrderID := instruction.Dispatch.OrderID - 100
	otherRecord := instruction.Dispatch
	otherRecord.ID -= 100
	otherRecord.OrderID = otherOrderID
	otherRecord.DispatchNo = instruction.Dispatch.DispatchNo + "-other"
	if err := repository.data.pool.QueryRow(ctx, "INSERT INTO public.trip_orders (id) VALUES ($1) RETURNING id", otherOrderID).Scan(&otherOrderID); err != nil {
		t.Fatalf("insert collision trip order: %v", err)
	}
	if err := repository.Create(ctx, otherRecord); err != nil {
		t.Fatalf("create collision dispatch record: %v", err)
	}
	if _, err := repository.data.pool.Exec(ctx, `INSERT INTO public.dispatch_outbox
		(id, dispatch_id, idempotency_key, payload, status) VALUES ($1, $2, $3, '{}'::jsonb, 0)`,
		outboxID, otherRecord.ID, otherRecord.DispatchNo); err != nil {
		t.Fatalf("insert collision outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.dispatch_outbox WHERE id = $1", outboxID)
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.dispatch_records WHERE id = $1", otherRecord.ID)
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.trip_orders WHERE id = $1", otherOrderID)
	})
}

func setupOutboxInstruction(t *testing.T) (*DispatchRepository, context.Context, biz.VehicleDispatchInstruction) {
	t.Helper()
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	vehicleID, secondVehicleID := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, vehicleID, secondVehicleID)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)
	record := testDispatchRecord(base+30, orderID, vehicleID, 1)
	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("create test outbox ID generator: %v", err)
	}
	repository := NewDispatchRepository(&Data{pool: pool, dispatchIDGenerator: generator})
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("create dispatch record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.dispatch_outbox WHERE dispatch_id = $1", record.ID)
	})
	return repository, ctx, biz.VehicleDispatchInstruction{
		Dispatch:             record,
		Pickup:               biz.DispatchLocation{StationID: 101, Longitude: 116.397128, Latitude: 39.916527, Address: "起点"},
		Dropoff:              biz.DispatchLocation{StationID: 102, Longitude: 116.407128, Latitude: 39.926527, Address: "终点"},
		ConfirmationDeadline: time.Date(2026, time.October, 28, 10, 0, 30, 0, time.UTC),
	}
}

func dispatchOutboxQueuerOf(t *testing.T, repository *DispatchRepository) interface {
	Queue(context.Context, biz.VehicleDispatchInstruction) (biz.DispatchRecord, error)
} {
	t.Helper()
	queuer, ok := any(repository).(interface {
		Queue(context.Context, biz.VehicleDispatchInstruction) (biz.DispatchRecord, error)
	})
	if !ok {
		t.Fatal("DispatchRepository does not implement atomic dispatch outbox queueing")
	}
	return queuer
}

func assertOutboxCount(t *testing.T, ctx context.Context, repository *DispatchRepository, dispatchID int64, want int) {
	t.Helper()
	var actual int
	err := repository.data.pool.QueryRow(ctx, "SELECT count(*) FROM public.dispatch_outbox WHERE dispatch_id = $1", dispatchID).Scan(&actual)
	if err != nil {
		t.Fatalf("count dispatch outbox: %v", err)
	}
	if actual != want {
		t.Errorf("outbox count = %d, want %d", actual, want)
	}
}

func dispatchOutboxProcessorOf(t *testing.T, repository *DispatchRepository) interface {
	ProcessOutboxOnce(context.Context, biz.VehicleDispatchGateway, time.Time, time.Duration) (bool, error)
} {
	t.Helper()
	processor, ok := any(repository).(interface {
		ProcessOutboxOnce(context.Context, biz.VehicleDispatchGateway, time.Time, time.Duration) (bool, error)
	})
	if !ok {
		t.Fatal("DispatchRepository does not implement dispatch outbox processing")
	}
	return processor
}

type fixedOutboxIDGenerator struct {
	id int64
}

func (generator fixedOutboxIDGenerator) New() int64 {
	return generator.id
}

type acceptingDispatchGateway struct{}

func (acceptingDispatchGateway) Send(context.Context, biz.VehicleDispatchInstruction) (bool, error) {
	return true, nil
}

type failingDispatchGateway struct{}

func (failingDispatchGateway) Send(context.Context, biz.VehicleDispatchInstruction) (bool, error) {
	return false, errors.New("module 4 unavailable")
}

type rejectingDispatchGateway struct{}

func (rejectingDispatchGateway) Send(context.Context, biz.VehicleDispatchInstruction) (bool, error) {
	return false, nil
}
