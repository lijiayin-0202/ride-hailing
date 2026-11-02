package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatchUsecaseCreatesFirstAttemptForEligibleVehicle(t *testing.T) {
	// 若实现错误地跳过首辆有资格的候选车、生成错误的尝试次数或提前写入派单时间，本测试应失败。
	repository := &fakeDispatchRecordRepository{}
	usecase := NewDispatchUsecase(
		repository,
		&fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 101, Score: 9.8}}},
		fixedDispatchNumberGenerator{number: "DSP-202610270001"},
		fixedDispatchIDGenerator{id: 1001},
		fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
	)

	result, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID:          1,
		ServiceAreaID:    10,
		Schedulable:      true,
		DispatchStrategy: "test-strategy",
	})
	if err != nil {
		t.Fatalf("CreateAttempt() error = %v", err)
	}

	if result.Record.AttemptNo != 1 {
		t.Errorf("AttemptNo = %d, want 1", result.Record.AttemptNo)
	}
	if result.Record.VehicleID != 101 {
		t.Errorf("VehicleID = %d, want 101", result.Record.VehicleID)
	}
	if result.Record.ID != 1001 {
		t.Errorf("ID = %d, want 1001", result.Record.ID)
	}
	if result.Record.DispatchStrategy != "test-strategy" {
		t.Errorf("DispatchStrategy = %q, want test-strategy", result.Record.DispatchStrategy)
	}
	if result.Record.Status != DispatchStatusCreated {
		t.Errorf("Status = %d, want %d", result.Record.Status, DispatchStatusCreated)
	}
	if result.Record.DispatchedAt != nil {
		t.Errorf("DispatchedAt = %v, want nil before actual vehicle dispatch", result.Record.DispatchedAt)
	}
}

func TestDispatchUsecaseRejectsFourthAttemptWithoutReadingCandidates(t *testing.T) {
	repository := &fakeDispatchRecordRepository{attemptCount: 3}
	candidates := &fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 101}}}
	usecase := NewDispatchUsecase(
		repository,
		candidates,
		fixedDispatchNumberGenerator{number: "DSP-202610270002"},
		fixedDispatchIDGenerator{id: 1002},
		fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
	)

	_, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID: 1, ServiceAreaID: 10, Schedulable: true, DispatchStrategy: "test-strategy",
	})
	if !errors.Is(err, ErrDispatchAttemptLimitReached) {
		t.Fatalf("CreateAttempt() error = %v, want ErrDispatchAttemptLimitReached", err)
	}
	if candidates.calls != 0 {
		t.Errorf("candidate repository calls = %d, want 0 after attempt limit", candidates.calls)
	}
	if repository.createCalls != 0 {
		t.Errorf("created records = %d, want 0 after attempt limit", repository.createCalls)
	}
}

func TestDispatchUsecaseReturnsNoEligibleVehicle(t *testing.T) {
	usecase := NewDispatchUsecase(
		&fakeDispatchRecordRepository{},
		&fakeCandidateVehicleRepository{},
		fixedDispatchNumberGenerator{number: "DSP-202610270003"},
		fixedDispatchIDGenerator{id: 1003},
		fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
	)

	_, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID: 1, ServiceAreaID: 10, Schedulable: true, DispatchStrategy: "test-strategy",
	})
	if !errors.Is(err, ErrNoEligibleVehicle) {
		t.Fatalf("CreateAttempt() error = %v, want ErrNoEligibleVehicle", err)
	}
}

func TestDispatchUsecaseRejectsCancelledOrder(t *testing.T) {
	repository := &fakeDispatchRecordRepository{}
	candidates := &fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 101}}}
	usecase := NewDispatchUsecase(
		repository,
		candidates,
		fixedDispatchNumberGenerator{number: "DSP-202610270004"},
		fixedDispatchIDGenerator{id: 1004},
		fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
	)

	_, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID: 1, ServiceAreaID: 10, Schedulable: true, Cancelled: true, DispatchStrategy: "test-strategy",
	})
	if !errors.Is(err, ErrOrderNotSchedulable) {
		t.Fatalf("CreateAttempt() error = %v, want ErrOrderNotSchedulable", err)
	}
	if repository.countCalls != 0 || candidates.calls != 0 || repository.createCalls != 0 {
		t.Fatal("cancelled order must not access dispatch persistence or candidates")
	}
}

func TestDispatchUsecaseRejectsOrderMarkedNotSchedulable(t *testing.T) {
	repository := &fakeDispatchRecordRepository{}
	candidates := &fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 101}}}
	usecase := NewDispatchUsecase(
		repository,
		candidates,
		fixedDispatchNumberGenerator{number: "DSP-202610270006"},
		fixedDispatchIDGenerator{id: 1006},
		fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
	)

	_, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID: 1, ServiceAreaID: 10, Schedulable: false, DispatchStrategy: "test-strategy",
	})
	if !errors.Is(err, ErrOrderNotSchedulable) {
		t.Fatalf("CreateAttempt() error = %v, want ErrOrderNotSchedulable", err)
	}
	if repository.countCalls != 0 || candidates.calls != 0 || repository.createCalls != 0 {
		t.Fatal("not-schedulable order must not access dispatch persistence or candidates")
	}
}

func TestDispatchUsecaseRejectsMissingDispatchIdentifiers(t *testing.T) {
	testCases := []struct {
		name    string
		request DispatchRequest
	}{
		{
			name:    "empty order ID",
			request: DispatchRequest{ServiceAreaID: 10, Schedulable: true, DispatchStrategy: "test-strategy"},
		},
		{
			name:    "empty service area ID",
			request: DispatchRequest{OrderID: 1, Schedulable: true, DispatchStrategy: "test-strategy"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &fakeDispatchRecordRepository{}
			candidates := &fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 101}}}
			usecase := NewDispatchUsecase(
				repository,
				candidates,
				fixedDispatchNumberGenerator{number: "DSP-202610270007"},
				fixedDispatchIDGenerator{id: 1007},
				fixedClock{now: time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)},
			)

			_, err := usecase.CreateAttempt(context.Background(), testCase.request)
			if !errors.Is(err, ErrOrderNotSchedulable) {
				t.Fatalf("CreateAttempt() error = %v, want ErrOrderNotSchedulable", err)
			}
			if repository.countCalls != 0 || candidates.calls != 0 || repository.createCalls != 0 {
				t.Fatal("request without required IDs must not access dispatch persistence or candidates")
			}
		})
	}
}

func TestDispatchUsecaseCreatesThirdAttemptAndThirtySecondDeadline(t *testing.T) {
	now := time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)
	repository := &fakeDispatchRecordRepository{attemptCount: 2}
	usecase := NewDispatchUsecase(
		repository,
		&fakeCandidateVehicleRepository{vehicles: []CandidateVehicle{{ID: 102, Score: 8.1}}},
		fixedDispatchNumberGenerator{number: "DSP-202610270005"},
		fixedDispatchIDGenerator{id: 1005},
		fixedClock{now: now},
	)

	result, err := usecase.CreateAttempt(context.Background(), DispatchRequest{
		OrderID: 1, ServiceAreaID: 10, Schedulable: true, DispatchStrategy: "test-strategy",
	})
	if err != nil {
		t.Fatalf("CreateAttempt() error = %v", err)
	}
	if result.Record.AttemptNo != 3 {
		t.Errorf("AttemptNo = %d, want 3", result.Record.AttemptNo)
	}
	if result.RemainingAttempts != 0 {
		t.Errorf("RemainingAttempts = %d, want 0", result.RemainingAttempts)
	}
	wantDeadline := time.Date(2026, time.October, 27, 10, 0, 30, 0, time.UTC)
	if !result.ConfirmationDeadline.Equal(wantDeadline) {
		t.Errorf("ConfirmationDeadline = %v, want %v", result.ConfirmationDeadline, wantDeadline)
	}
}

type fakeDispatchRecordRepository struct {
	attemptCount        int
	attemptedVehicleIDs []int64
	countCalls          int
	attemptedCalls      int
	createCalls         int
}

func (repository *fakeDispatchRecordRepository) CountAttempts(context.Context, int64) (int, error) {
	repository.countCalls++
	return repository.attemptCount, nil
}

func (repository *fakeDispatchRecordRepository) Create(context.Context, DispatchRecord) error {
	repository.createCalls++
	return nil
}

func (repository *fakeDispatchRecordRepository) ListAttemptedVehicleIDs(context.Context, int64) ([]int64, error) {
	repository.attemptedCalls++
	return repository.attemptedVehicleIDs, nil
}

type fakeCandidateVehicleRepository struct {
	vehicles           []CandidateVehicle
	excludedVehicleIDs []int64
	calls              int
}

func (repository *fakeCandidateVehicleRepository) ListEligible(_ context.Context, _ int64, excludedVehicleIDs []int64) ([]CandidateVehicle, error) {
	repository.calls++
	repository.excludedVehicleIDs = excludedVehicleIDs
	return repository.vehicles, nil
}

type fixedDispatchNumberGenerator struct {
	number string
}

func (generator fixedDispatchNumberGenerator) New() string {
	return generator.number
}

type fixedDispatchIDGenerator struct {
	id int64
}

func (generator fixedDispatchIDGenerator) New() int64 {
	return generator.id
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}
