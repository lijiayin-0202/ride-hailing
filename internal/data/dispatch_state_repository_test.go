package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestDispatchRepositoryMarksCreatedAttemptPendingOnce(t *testing.T) {
	repository, ctx, record := setupDispatchStateRecord(t)
	at := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)

	updated, err := repository.MarkDispatched(ctx, record.ID, at)
	if err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	if updated.Status != biz.DispatchStatusPending || updated.DispatchedAt == nil || !updated.DispatchedAt.Equal(at) {
		t.Errorf("MarkDispatched() = %#v, want pending at %v", updated, at)
	}
	if _, err := repository.MarkDispatched(ctx, record.ID, at); !errors.Is(err, ErrDispatchStateConflict) {
		t.Errorf("second MarkDispatched() error = %v, want ErrDispatchStateConflict", err)
	}
}

func TestDispatchRepositoryResolvesPendingAttempt(t *testing.T) {
	repository, ctx, record := setupDispatchStateRecord(t)
	at := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)
	if _, err := repository.MarkDispatched(ctx, record.ID, at); err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	responseAt := at.Add(2 * time.Second)
	resolution := biz.DispatchResolution{DispatchID: record.ID, Status: biz.DispatchStatusRejected, FailureReason: "vehicle rejected"}

	updated, err := repository.Resolve(ctx, resolution, responseAt)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if updated.Status != biz.DispatchStatusRejected || updated.RespondedAt == nil || !updated.RespondedAt.Equal(responseAt) || updated.FailureReason != resolution.FailureReason {
		t.Errorf("Resolve() = %#v, want rejected response", updated)
	}
	duplicate, err := repository.Resolve(ctx, resolution, responseAt)
	if err != nil || duplicate.Status != biz.DispatchStatusRejected {
		t.Errorf("duplicate Resolve() = %#v, %v; want successful rejected result", duplicate, err)
	}
}

func TestDispatchRepositoryRejectsResolutionWithMismatchedOrderOrVehicle(t *testing.T) {
	repository, ctx, record := setupDispatchStateRecord(t)
	at := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)
	if _, err := repository.MarkDispatched(ctx, record.ID, at); err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	resolution := biz.DispatchResolution{DispatchID: record.ID, OrderID: record.OrderID + 1, VehicleID: record.VehicleID, Status: biz.DispatchStatusAccepted}
	if _, err := repository.Resolve(ctx, resolution, at.Add(time.Second)); !errors.Is(err, ErrDispatchStateConflict) {
		t.Errorf("Resolve() error = %v, want ErrDispatchStateConflict", err)
	}
}

func TestDispatchRepositoryListsAttemptedVehiclesAndTimeouts(t *testing.T) {
	repository, ctx, first := setupDispatchStateRecord(t)
	second := first
	second.ID++
	second.DispatchNo += "-second"
	second.AttemptNo = 2
	second.VehicleID++
	old := time.Date(2026, time.October, 28, 9, 59, 29, 0, time.UTC)
	newer := old.Add(time.Second)
	if _, err := repository.MarkDispatched(ctx, first.ID, old); err != nil {
		t.Fatalf("MarkDispatched(first) error = %v", err)
	}
	if _, err := repository.Resolve(ctx, biz.DispatchResolution{DispatchID: first.ID, Status: biz.DispatchStatusRejected}, old.Add(time.Millisecond)); err != nil {
		t.Fatalf("Resolve(first) error = %v", err)
	}
	if err := repository.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if _, err := repository.MarkDispatched(ctx, second.ID, newer); err != nil {
		t.Fatalf("MarkDispatched(second) error = %v", err)
	}

	vehicles, err := repository.ListAttemptedVehicleIDs(ctx, first.OrderID)
	if err != nil || len(vehicles) != 2 || vehicles[0] != first.VehicleID || vehicles[1] != second.VehicleID {
		t.Errorf("ListAttemptedVehicleIDs() = %#v, %v", vehicles, err)
	}
	timeouts, err := repository.ListPendingBefore(ctx, newer)
	if err != nil || len(timeouts) != 1 || timeouts[0].ID != second.ID {
		t.Errorf("ListPendingBefore() = %#v, %v", timeouts, err)
	}
}

func setupDispatchStateRecord(t *testing.T) (*DispatchRepository, context.Context, biz.DispatchRecord) {
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
	repository := NewDispatchRepository(&Data{pool: pool})
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return repository, ctx, record
}
