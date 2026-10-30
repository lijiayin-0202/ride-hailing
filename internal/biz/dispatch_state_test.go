package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatchStateUsecaseMarksCreatedAttemptAsPending(t *testing.T) {
	now := time.Date(2026, time.October, 28, 9, 0, 0, 0, time.UTC)
	repository := &fakeDispatchStateRepository{}
	usecase := NewDispatchStateUsecase(repository, fixedClock{now: now})

	got, err := usecase.MarkDispatched(context.Background(), 11)
	if err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	if got.Status != DispatchStatusPending || got.DispatchedAt == nil || !got.DispatchedAt.Equal(now) {
		t.Errorf("MarkDispatched() = %#v, want pending record at %v", got, now)
	}
}

func TestDispatchStateUsecaseResolvesPendingAttemptOnce(t *testing.T) {
	now := time.Date(2026, time.October, 28, 9, 0, 0, 0, time.UTC)
	repository := &fakeDispatchStateRepository{}
	usecase := NewDispatchStateUsecase(repository, fixedClock{now: now})

	got, err := usecase.Resolve(context.Background(), DispatchResolution{DispatchID: 12, Status: DispatchStatusRejected, FailureReason: "vehicle rejected"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Status != DispatchStatusRejected || got.RespondedAt == nil || !got.RespondedAt.Equal(now) || got.FailureReason != "vehicle rejected" {
		t.Errorf("Resolve() = %#v, want rejected record at %v", got, now)
	}
}

func TestDispatchStateUsecaseRejectsInvalidResolution(t *testing.T) {
	usecase := NewDispatchStateUsecase(&fakeDispatchStateRepository{}, fixedClock{})

	_, err := usecase.Resolve(context.Background(), DispatchResolution{DispatchID: 13, Status: DispatchStatusPending})
	if !errors.Is(err, ErrInvalidDispatchResolution) {
		t.Fatalf("Resolve() error = %v, want ErrInvalidDispatchResolution", err)
	}
}

func TestDispatchStateUsecaseFindsTimeoutsAfterThirtySeconds(t *testing.T) {
	now := time.Date(2026, time.October, 28, 9, 1, 0, 0, time.UTC)
	repository := &fakeDispatchStateRepository{}
	usecase := NewDispatchStateUsecase(repository, fixedClock{now: now})

	_, err := usecase.ListTimeouts(context.Background())
	if err != nil {
		t.Fatalf("ListTimeouts() error = %v", err)
	}
	want := now.Add(-30 * time.Second)
	if !repository.timeoutBefore.Equal(want) {
		t.Errorf("timeout cutoff = %v, want %v", repository.timeoutBefore, want)
	}
}

func TestDispatchUsecaseExcludesPreviouslyTriedVehicles(t *testing.T) {
	records := &fakeDispatchRecordRepository{attemptCount: 1, attemptedVehicleIDs: []int64{101}}
	candidates := &fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 102}}}
	usecase := NewDispatchUsecase(records, candidates, fixedDispatchNumberGenerator{number: "DSP-2"}, fixedDispatchIDGenerator{id: 2}, fixedClock{})

	_, err := usecase.CreateAttempt(context.Background(), DispatchRequest{OrderID: 1, ServiceAreaID: 2, Schedulable: true})
	if err != nil {
		t.Fatalf("CreateAttempt() error = %v", err)
	}
	if records.attemptedCalls != 1 || candidates.calls != 1 {
		t.Fatalf("history calls:%d candidate calls:%d, want 1:1", records.attemptedCalls, candidates.calls)
	}
	if len(candidates.excludedVehicleIDs) != 1 || candidates.excludedVehicleIDs[0] != 101 {
		t.Errorf("candidate exclusions = %#v, want [101]", candidates.excludedVehicleIDs)
	}
}

func TestDispatchStateUsecaseExpiresPendingAttempts(t *testing.T) {
	now := time.Date(2026, time.October, 28, 10, 0, 0, 0, time.UTC)
	repository := &fakeDispatchStateRepository{pending: []DispatchRecord{{ID: 31, OrderID: 41, VehicleID: 51}}}
	usecase := NewDispatchStateUsecase(repository, fixedClock{now: now})

	count, err := usecase.ExpireTimeouts(context.Background())
	if err != nil {
		t.Fatalf("ExpireTimeouts() error = %v", err)
	}
	if count != 1 || repository.resolution.Status != DispatchStatusTimedOut || repository.resolution.DispatchID != 31 {
		t.Fatalf("ExpireTimeouts() count=%d resolution=%#v", count, repository.resolution)
	}
}

type fakeDispatchStateRepository struct {
	timeoutBefore time.Time
	pending       []DispatchRecord
	resolution    DispatchResolution
}

func (repository *fakeDispatchStateRepository) MarkDispatched(_ context.Context, id int64, at time.Time) (DispatchRecord, error) {
	return DispatchRecord{ID: id, Status: DispatchStatusPending, DispatchedAt: &at}, nil
}

func (repository *fakeDispatchStateRepository) Resolve(_ context.Context, resolution DispatchResolution, at time.Time) (DispatchRecord, error) {
	repository.resolution = resolution
	return DispatchRecord{ID: resolution.DispatchID, Status: resolution.Status, FailureReason: resolution.FailureReason, RespondedAt: &at}, nil
}

func (repository *fakeDispatchStateRepository) ListPendingBefore(_ context.Context, before time.Time) ([]DispatchRecord, error) {
	repository.timeoutBefore = before
	return repository.pending, nil
}
