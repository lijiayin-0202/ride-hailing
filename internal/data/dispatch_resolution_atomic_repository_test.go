package data

import (
	"context"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestDispatchRepositoryResolveAndQueueRejectionCreatesEventAndJob(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	t.Cleanup(func() {
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id = $1", instruction.Dispatch.ID)
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id = $1", instruction.Dispatch.ID)
	})
	if _, err := repository.Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	resolution := biz.DispatchResolution{DispatchID: instruction.Dispatch.ID, OrderID: instruction.Dispatch.OrderID,
		VehicleID: instruction.Dispatch.VehicleID, Status: biz.DispatchStatusRejected, FailureReason: "vehicle refused"}
	record, err := repository.ResolveAndQueue(ctx, resolution, time.Now().UTC())
	if err != nil {
		t.Fatalf("ResolveAndQueue() error = %v", err)
	}
	if record.Status != biz.DispatchStatusRejected {
		t.Fatalf("ResolveAndQueue() = %#v, want rejected", record)
	}
	assertResolutionEventAndJob(t, ctx, repository, record.ID, biz.OrderDispatchEventRejected, 1)

	if _, err := repository.ResolveAndQueue(ctx, resolution, time.Now().UTC()); err != nil {
		t.Fatalf("duplicate ResolveAndQueue() error = %v", err)
	}
	assertResolutionEventAndJob(t, ctx, repository, record.ID, biz.OrderDispatchEventRejected, 1)
}

func assertResolutionEventAndJob(t *testing.T, ctx context.Context, repository *DispatchRepository, dispatchID int64, eventType biz.OrderDispatchEventType, wantJobs int) {
	t.Helper()
	var events, jobs int
	if err := repository.data.pool.QueryRow(ctx, "SELECT count(*) FROM public.order_dispatch_outbox WHERE dispatch_id=$1 AND event_type=$2", dispatchID, eventType).Scan(&events); err != nil {
		t.Fatalf("count rejection event: %v", err)
	}
	if err := repository.data.pool.QueryRow(ctx, "SELECT count(*) FROM public.dispatch_reassignment_jobs WHERE trigger_dispatch_id=$1", dispatchID).Scan(&jobs); err != nil {
		t.Fatalf("count reassignment jobs: %v", err)
	}
	if events != 1 || jobs != wantJobs {
		t.Fatalf("resolution facts events=%d jobs=%d, want 1 and %d", events, jobs, wantJobs)
	}
}
