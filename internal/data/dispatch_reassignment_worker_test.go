package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestReassignmentWorkerAssignsSecondVehicleAfterRejection(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	t.Cleanup(func() {
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id = $1", instruction.Dispatch.ID)
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id = $1", instruction.Dispatch.ID)
	})
	areaID := dispatchAreaID(t, ctx, repository, instruction.Dispatch.VehicleID)
	seedDispatchPositions(t, ctx, repository, areaID, instruction.Dispatch.VehicleID)
	if _, err := repository.Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	_, err := repository.ResolveAndQueue(ctx, biz.DispatchResolution{DispatchID: instruction.Dispatch.ID,
		OrderID: instruction.Dispatch.OrderID, VehicleID: instruction.Dispatch.VehicleID,
		Status: biz.DispatchStatusRejected, FailureReason: "rejected"}, time.Now().UTC())
	if err != nil {
		t.Fatalf("ResolveAndQueue() error = %v", err)
	}
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.dispatch_reassignment_jobs
		SET next_attempt_at=$2 WHERE trigger_dispatch_id=$1`, instruction.Dispatch.ID, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("prioritize reassignment job: %v", err)
	}
	snapshot := biz.OrderDispatchSnapshot{OrderID: instruction.Dispatch.OrderID, ServiceAreaID: areaID, Schedulable: true,
		Pickup: biz.DispatchLocation{Longitude: 116.397128, Latitude: 39.916527}, Dropoff: biz.DispatchLocation{Longitude: 116.407128, Latitude: 39.926527}}
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin candidate inspection: %v", err)
	}
	candidate, candidateErr := selectNearestCandidate(ctx, tx, snapshot, 30)
	_ = tx.Rollback(ctx)
	if candidateErr != nil {
		t.Fatalf("select second candidate: %v", candidateErr)
	}
	if candidate.ID == instruction.Dispatch.VehicleID {
		t.Fatalf("selected historical vehicle %d", candidate.ID)
	}
	application := &recordingReassignmentApplication{inner: biz.NewAutoDispatchUsecase(&testOrderQueryGateway{snapshot: snapshot}, repository, biz.NewSystemClock())}
	processed, err := repository.ProcessReassignmentOnce(ctx, application, time.Now().UTC(), time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessReassignmentOnce() = %t, %v", processed, err)
	}
	assertSecondAttempt(t, ctx, repository, instruction.Dispatch, application.err)
}

func TestReassignmentWorkerWritesExhaustedWhenAttemptLimitReached(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	t.Cleanup(func() {
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id = $1", instruction.Dispatch.ID)
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id = $1", instruction.Dispatch.ID)
	})
	if _, err := repository.Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if _, err := repository.ResolveAndQueue(ctx, biz.DispatchResolution{DispatchID: instruction.Dispatch.ID,
		OrderID: instruction.Dispatch.OrderID, VehicleID: instruction.Dispatch.VehicleID,
		Status: biz.DispatchStatusTimedOut, FailureReason: "confirmation timeout"}, time.Now().UTC()); err != nil {
		t.Fatalf("ResolveAndQueue() error = %v", err)
	}
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.dispatch_reassignment_jobs
		SET next_attempt_at=$2 WHERE trigger_dispatch_id=$1`, instruction.Dispatch.ID, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("prioritize reassignment job: %v", err)
	}
	processed, err := repository.ProcessReassignmentOnce(ctx, failingReassignmentApplication{}, time.Now().UTC(), time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessReassignmentOnce() = %t, %v", processed, err)
	}
	assertResolutionEventAndJob(t, ctx, repository, instruction.Dispatch.ID, biz.OrderDispatchEventExhausted, 1)
	var status int16
	if err := repository.data.pool.QueryRow(ctx, "SELECT status FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id=$1", instruction.Dispatch.ID).Scan(&status); err != nil {
		t.Fatalf("read exhausted job: %v", err)
	}
	if status != reassignmentCompleted {
		t.Fatalf("exhausted job status=%d, want completed", status)
	}
}

func seedDispatchPositions(t *testing.T, ctx context.Context, repository *DispatchRepository, areaID, firstVehicleID int64) {
	t.Helper()
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.vehicle_status_logs SET current_longitude=116.397128,current_latitude=39.916527 WHERE vehicle_id=$1`, firstVehicleID); err != nil {
		t.Fatalf("seed first vehicle location: %v", err)
	}
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.vehicle_status_logs log SET current_longitude=116.407128,current_latitude=39.916527
		FROM public.vehicles vehicle WHERE log.vehicle_id=vehicle.id AND vehicle.service_area_id=$1 AND vehicle.id<>$2`, areaID, firstVehicleID); err != nil {
		t.Fatalf("seed second vehicle location: %v", err)
	}
}

func dispatchAreaID(t *testing.T, ctx context.Context, repository *DispatchRepository, vehicleID int64) int64 {
	t.Helper()
	var areaID int64
	if err := repository.data.pool.QueryRow(ctx, "SELECT service_area_id FROM public.vehicles WHERE id=$1", vehicleID).Scan(&areaID); err != nil {
		t.Fatalf("read vehicle area: %v", err)
	}
	return areaID
}

func assertSecondAttempt(t *testing.T, ctx context.Context, repository *DispatchRepository, first biz.DispatchRecord, applicationErr error) {
	t.Helper()
	var vehicleID int64
	var attemptNo int16
	if err := repository.data.pool.QueryRow(ctx, `SELECT vehicle_id,attempt_no FROM public.dispatch_records
		WHERE order_id=$1 AND attempt_no=2`, first.OrderID).Scan(&vehicleID, &attemptNo); err != nil {
		var status int16
		var lastError *string
		_ = repository.data.pool.QueryRow(ctx, `SELECT status,last_error FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id=$1`, first.ID).Scan(&status, &lastError)
		t.Fatalf("read second attempt: %v; reassignment status=%d lastError=%v applicationError=%v", err, status, lastError, applicationErr)
	}
	if vehicleID == first.VehicleID || attemptNo != 2 {
		t.Fatalf("second attempt vehicle=%d attempt=%d, want different vehicle and attempt 2", vehicleID, attemptNo)
	}
}

type recordingReassignmentApplication struct {
	inner ReassignmentApplication
	err   error
}

func (application *recordingReassignmentApplication) Assign(ctx context.Context, orderID int64) (biz.AssignDispatchResult, error) {
	result, err := application.inner.Assign(ctx, orderID)
	application.err = err
	return result, err
}

type failingReassignmentApplication struct{}

func (failingReassignmentApplication) Assign(context.Context, int64) (biz.AssignDispatchResult, error) {
	return biz.AssignDispatchResult{}, errors.Join(biz.ErrDispatchAttemptLimitReached)
}

type testOrderQueryGateway struct {
	snapshot biz.OrderDispatchSnapshot
}

func (gateway *testOrderQueryGateway) GetDispatchableOrder(context.Context, int64) (biz.OrderDispatchSnapshot, error) {
	return gateway.snapshot, nil
}
