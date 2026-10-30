package biz

import (
	"context"
	"testing"
	"time"
)

func TestAutoDispatchUsecaseReusesActiveAttempt(t *testing.T) {
	now := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)
	repository := &fakePendingAttemptRepository{active: DispatchRecord{
		ID: 90, DispatchNo: "DSP-90", OrderID: 81, VehicleID: 101, AttemptNo: 1,
		Status: DispatchStatusPending, DispatchedAt: pointerToTime(now),
	}}
	usecase := NewAutoDispatchUsecase(
		&fakeOrderDispatchQueryGateway{snapshot: schedulableOrderSnapshot(81)},
		repository,
		fixedClock{now: now},
	)

	result, err := usecase.Assign(context.Background(), 81)
	if err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if !result.ReusedActiveAttempt || result.Record.ID != 90 || !result.ConfirmationDeadline.Equal(now.Add(confirmationTimeout)) {
		t.Fatalf("Assign() = %#v, want reused active attempt", result)
	}
	if repository.createCalls != 0 {
		t.Fatalf("CreatePending() calls = %d, want 0 when an active attempt exists", repository.createCalls)
	}
}

func TestAutoDispatchUsecaseCreatesPendingAttemptWhenNoActiveAttemptExists(t *testing.T) {
	now := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)
	repository := &fakePendingAttemptRepository{createResult: AssignDispatchResult{Record: DispatchRecord{ID: 91, OrderID: 81, VehicleID: 102, Status: DispatchStatusPending}}}
	usecase := NewAutoDispatchUsecase(
		&fakeOrderDispatchQueryGateway{snapshot: schedulableOrderSnapshot(81)},
		repository,
		fixedClock{now: now},
	)

	result, err := usecase.Assign(context.Background(), 81)
	if err != nil {
		t.Fatalf("Assign() error = %v", err)
	}
	if result.Record.ID != 91 || result.ReusedActiveAttempt || repository.createCalls != 1 || repository.input.Snapshot.OrderID != 81 {
		t.Fatalf("Assign() = %#v input = %#v, want newly created pending attempt", result, repository.input)
	}
}

type fakePendingAttemptRepository struct {
	active       DispatchRecord
	createCalls  int
	input        PendingDispatchAttempt
	createResult AssignDispatchResult
}

func (repository *fakePendingAttemptRepository) FindActive(_ context.Context, _ int64) (DispatchRecord, bool, error) {
	return repository.active, repository.active.ID > 0, nil
}

func (repository *fakePendingAttemptRepository) CreatePending(_ context.Context, input PendingDispatchAttempt) (AssignDispatchResult, error) {
	repository.createCalls++
	repository.input = input
	return repository.createResult, nil
}

type fakeOrderDispatchQueryGateway struct {
	snapshot OrderDispatchSnapshot
}

func (gateway *fakeOrderDispatchQueryGateway) GetDispatchableOrder(_ context.Context, _ int64) (OrderDispatchSnapshot, error) {
	return gateway.snapshot, nil
}

func schedulableOrderSnapshot(orderID int64) OrderDispatchSnapshot {
	return OrderDispatchSnapshot{OrderID: orderID, ServiceAreaID: 9, Schedulable: true}
}

func pointerToTime(value time.Time) *time.Time {
	return &value
}
