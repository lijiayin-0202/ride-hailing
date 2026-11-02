package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestOrderDispatchOutboxProcessorDeliversEvent(t *testing.T) {
	repository, ctx, event := setupOrderDispatchOutboxEvent(t)
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	processed, err := repository.ProcessOrderDispatchOutboxOnce(ctx, acceptingOrderDispatchGateway{}, now, time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOrderDispatchOutboxOnce() = %t, %v", processed, err)
	}
	assertOrderOutboxDelivery(t, ctx, repository, event.DispatchID, orderDispatchOutboxDelivered, 0, now)
}

func TestOrderDispatchOutboxProcessorBacksOffGatewayFailure(t *testing.T) {
	repository, ctx, event := setupOrderDispatchOutboxEvent(t)
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	processed, err := repository.ProcessOrderDispatchOutboxOnce(ctx, failingOrderDispatchGateway{}, now, time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOrderDispatchOutboxOnce() = %t, %v", processed, err)
	}
	assertOrderOutboxDelivery(t, ctx, repository, event.DispatchID, orderDispatchOutboxPending, 1, now.Add(time.Second))
}

func TestOrderDispatchOutboxProcessorReclaimsExpiredLease(t *testing.T) {
	repository, ctx, event := setupOrderDispatchOutboxEvent(t)
	now := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.order_dispatch_outbox
		SET status = 1, lease_expires_at = $2 WHERE dispatch_id = $1`, event.DispatchID, now.Add(-time.Second)); err != nil {
		t.Fatalf("seed expired lease: %v", err)
	}
	processed, err := repository.ProcessOrderDispatchOutboxOnce(ctx, acceptingOrderDispatchGateway{}, now, time.Second)
	if err != nil || !processed {
		t.Fatalf("ProcessOrderDispatchOutboxOnce() = %t, %v", processed, err)
	}
	assertOrderOutboxDelivery(t, ctx, repository, event.DispatchID, orderDispatchOutboxDelivered, 0, now)
}

func setupOrderDispatchOutboxEvent(t *testing.T) (*DispatchRepository, context.Context, biz.OrderDispatchEvent) {
	t.Helper()
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := repository.Queue(ctx, instruction); err != nil {
		t.Fatalf("queue dispatch: %v", err)
	}
	event := biz.OrderDispatchEvent{DispatchNo: instruction.Dispatch.DispatchNo, OrderID: instruction.Dispatch.OrderID,
		DispatchID: instruction.Dispatch.ID, VehicleID: instruction.Dispatch.VehicleID,
		Type: biz.OrderDispatchEventAttemptPending, OccurredAt: time.Now().UTC()}
	if err := repository.EnqueueOrderDispatchEvent(ctx, event); err != nil {
		t.Fatalf("EnqueueOrderDispatchEvent() error = %v", err)
	}
	if _, err := repository.data.pool.Exec(ctx, `UPDATE public.order_dispatch_outbox
		SET next_attempt_at = $2 WHERE dispatch_id = $1`, event.DispatchID, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("prioritize test order dispatch event: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repository.data.pool.Exec(context.Background(), "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id = $1", event.DispatchID)
	})
	return repository, ctx, event
}

func assertOrderOutboxDelivery(t *testing.T, ctx context.Context, repository *DispatchRepository, dispatchID int64, wantStatus int16, wantAttempts int, wantTime time.Time) {
	t.Helper()
	var status int16
	var attempts int
	var nextAttempt, deliveredAt *time.Time
	err := repository.data.pool.QueryRow(ctx, `SELECT status, attempt_count, next_attempt_at, delivered_at
		FROM public.order_dispatch_outbox WHERE dispatch_id = $1`, dispatchID).Scan(&status, &attempts, &nextAttempt, &deliveredAt)
	if err != nil {
		t.Fatalf("read order dispatch outbox: %v", err)
	}
	if status != wantStatus || attempts != wantAttempts {
		t.Fatalf("order outbox status=%d attempts=%d, want status=%d attempts=%d", status, attempts, wantStatus, wantAttempts)
	}
	if wantStatus == orderDispatchOutboxDelivered && (deliveredAt == nil || !deliveredAt.Equal(wantTime)) {
		t.Fatalf("order outbox deliveredAt=%v, want %v", deliveredAt, wantTime)
	}
	if wantStatus == orderDispatchOutboxPending && (nextAttempt == nil || !nextAttempt.Equal(wantTime)) {
		t.Fatalf("order outbox nextAttempt=%v, want %v", nextAttempt, wantTime)
	}
}

type acceptingOrderDispatchGateway struct{}

func (acceptingOrderDispatchGateway) Sync(context.Context, biz.OrderDispatchEvent) (bool, error) {
	return true, nil
}

type failingOrderDispatchGateway struct{}

func (failingOrderDispatchGateway) Sync(context.Context, biz.OrderDispatchEvent) (bool, error) {
	return false, errors.New("module 2 unavailable")
}
