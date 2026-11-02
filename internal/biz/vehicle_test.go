package biz

import (
	"context"
	"errors"
	"testing"
)

func TestVehicleUsecaseListSnapshotsRejectsInvalidArea(t *testing.T) {
	repository := &fakeVehicleRepository{}
	usecase := NewVehicleUsecase(repository)

	_, err := usecase.ListSnapshots(context.Background(), 0)
	if !errors.Is(err, ErrInvalidVehicleServiceArea) {
		t.Fatalf("ListSnapshots() error = %v, want ErrInvalidVehicleServiceArea", err)
	}
	if repository.called {
		t.Fatal("ListSnapshots() called repository for invalid area")
	}
}

func TestVehicleUsecaseListSnapshotsReadsRepository(t *testing.T) {
	want := []VehicleSnapshot{{Vehicle: Vehicle{ID: 101, ServiceAreaID: 9}}}
	repository := &fakeVehicleRepository{snapshots: want}
	usecase := NewVehicleUsecase(repository)

	got, err := usecase.ListSnapshots(context.Background(), 9)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if !repository.called || repository.serviceAreaID != 9 {
		t.Fatalf("repository call = called:%t area:%d, want true:9", repository.called, repository.serviceAreaID)
	}
	if len(got) != 1 || got[0].Vehicle.ID != want[0].Vehicle.ID {
		t.Errorf("ListSnapshots() = %#v, want %#v", got, want)
	}
}

func TestIsDispatchEligibleRequiresLatestHealthyIdleStatus(t *testing.T) {
	eligible := dispatchStatus(VehicleOnline, VehicleOperationIdle, true, "")
	if !IsDispatchEligible(&eligible) {
		t.Fatal("healthy idle vehicle should be dispatch eligible")
	}

	for name, status := range ineligibleStatuses() {
		t.Run(name, func(t *testing.T) {
			if IsDispatchEligible(status) {
				t.Errorf("IsDispatchEligible(%#v) = true, want false", status)
			}
		})
	}
}

func dispatchStatus(online, operation int16, accepts bool, fault string) VehicleRuntimeStatus {
	return VehicleRuntimeStatus{
		OnlineStatus: online, OperationStatus: operation,
		IsAcceptingDispatch: accepts, FaultCode: fault,
	}
}

func ineligibleStatuses() map[string]*VehicleRuntimeStatus {
	offline := dispatchStatus(VehicleOffline, VehicleOperationIdle, true, "")
	unknown := dispatchStatus(VehicleOnline, VehicleOperationUnknown, true, "")
	pickup := dispatchStatus(VehicleOnline, VehicleOperationPickup, true, "")
	passenger := dispatchStatus(VehicleOnline, VehicleOperationPassenger, true, "")
	charging := dispatchStatus(VehicleOnline, VehicleOperationCharging, true, "")
	maintenance := dispatchStatus(VehicleOnline, VehicleOperationMaintenance, true, "")
	rejected := dispatchStatus(VehicleOnline, VehicleOperationIdle, false, "")
	faulted := dispatchStatus(VehicleOnline, VehicleOperationIdle, true, "E-001")
	return map[string]*VehicleRuntimeStatus{
		"no_status": nil, "offline": &offline, "unknown": &unknown, "pickup": &pickup,
		"passenger": &passenger, "charging": &charging, "maintenance": &maintenance,
		"not_accepting": &rejected, "faulted": &faulted,
	}
}

type fakeVehicleRepository struct {
	called        bool
	serviceAreaID int64
	snapshots     []VehicleSnapshot
	err           error
}

func (repository *fakeVehicleRepository) ListSnapshots(_ context.Context, serviceAreaID int64) ([]VehicleSnapshot, error) {
	repository.called = true
	repository.serviceAreaID = serviceAreaID
	return repository.snapshots, repository.err
}
