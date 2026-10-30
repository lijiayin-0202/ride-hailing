package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStationUsecaseCreateRejectsInvalidCode(t *testing.T) {
	repository := &fakeStationRepository{}
	usecase := NewStationUsecase(repository, &fakeStationAreaRepository{})
	command := validStationCreateCommand()
	command.StationCode = "bad code"

	_, err := usecase.Create(context.Background(), command)
	if !errors.Is(err, ErrInvalidStation) {
		t.Errorf("Create() error = %v, want ErrInvalidStation", err)
	}
	if repository.createCalls != 0 {
		t.Errorf("Create() persistence calls = %d, want 0", repository.createCalls)
	}
}

func TestStationUsecaseCreateRejectsCoordinateOutsideChina(t *testing.T) {
	repository := &fakeStationRepository{}
	usecase := NewStationUsecase(repository, &fakeStationAreaRepository{})
	command := validStationCreateCommand()
	command.Longitude = 50

	_, err := usecase.Create(context.Background(), command)
	if !errors.Is(err, ErrInvalidStation) {
		t.Errorf("Create() error = %v, want ErrInvalidStation", err)
	}
}

func TestStationUsecaseUpdateRejectsCodeOrAreaChange(t *testing.T) {
	testCases := []UpdateStationCommand{
		{ID: 1, StationCode: "NEW-CODE"},
		{ID: 1, ServiceAreaID: 2},
	}
	for _, command := range testCases {
		t.Run("immutable field", func(t *testing.T) {
			repository := &fakeStationRepository{station: validStation()}
			usecase := NewStationUsecase(repository, &fakeStationAreaRepository{})

			_, err := usecase.Update(context.Background(), command)
			if !errors.Is(err, ErrImmutableStationField) {
				t.Errorf("Update() error = %v, want ErrImmutableStationField", err)
			}
		})
	}
}

func TestStationUsecaseChangeStatusRejectsInvalidValue(t *testing.T) {
	usecase := NewStationUsecase(&fakeStationRepository{}, &fakeStationAreaRepository{})

	_, err := usecase.ChangeStatus(context.Background(), 1, 9)
	if !errors.Is(err, ErrInvalidStation) {
		t.Errorf("ChangeStatus() error = %v, want ErrInvalidStation", err)
	}
}

func TestStationUsecaseListAvailableFiltersAreaTypeDirectionAndStatus(t *testing.T) {
	repository := &fakeStationRepository{stations: []Station{
		validStation(),
		{ID: 2, ServiceAreaID: 1, StationType: StationTypeRecommended, ServiceDirection: StationDirectionPickup, Status: StationStatusDisabled},
		{ID: 3, ServiceAreaID: 1, StationType: StationTypeVehicleBase, ServiceDirection: StationDirectionBoth, Status: StationStatusEnabled},
		{ID: 4, ServiceAreaID: 1, StationType: StationTypeNormal, ServiceDirection: StationDirectionDropoff, Status: StationStatusEnabled},
	}}
	usecase := NewStationUsecase(repository, &fakeStationAreaRepository{})

	stations, err := usecase.ListAvailable(context.Background(), 1, StationDirectionPickup)
	if err != nil {
		t.Fatalf("ListAvailable() error = %v", err)
	}
	if len(stations) != 1 || stations[0].ID != 1 {
		t.Errorf("ListAvailable() = %#v, want only station 1", stations)
	}
}

func validStationCreateCommand() CreateStationCommand {
	return CreateStationCommand{
		ServiceAreaID:    1,
		StationCode:      "BJ-YZ-001",
		StationName:      "亦庄上车点",
		Address:          "北京经济技术开发区示例路 1 号",
		Longitude:        116.4074,
		Latitude:         39.9042,
		StationType:      StationTypeNormal,
		ServiceDirection: StationDirectionBoth,
	}
}

func validStation() Station {
	command := validStationCreateCommand()
	return Station{
		ID: 1, ServiceAreaID: command.ServiceAreaID, StationCode: command.StationCode,
		StationName: command.StationName, Address: command.Address,
		Longitude: command.Longitude, Latitude: command.Latitude,
		StationType: command.StationType, ServiceDirection: command.ServiceDirection,
		Status: StationStatusEnabled,
	}
}

type fakeStationRepository struct {
	station     Station
	stations    []Station
	createCalls int
}

func (repository *fakeStationRepository) Create(_ context.Context, station Station) (Station, error) {
	repository.createCalls++
	return station, nil
}

func (repository *fakeStationRepository) Get(context.Context, int64) (Station, error) {
	return repository.station, nil
}

func (repository *fakeStationRepository) Update(_ context.Context, station Station) (Station, error) {
	return station, nil
}

func (repository *fakeStationRepository) ChangeStatus(_ context.Context, id int64, status int16) (Station, error) {
	return Station{ID: id, Status: status}, nil
}

func (repository *fakeStationRepository) List(context.Context, int64) ([]Station, error) {
	return repository.stations, nil
}

func (repository *fakeStationRepository) ListAvailable(context.Context, int64, int16) ([]Station, error) {
	return repository.stations, nil
}

type fakeStationAreaRepository struct{}

func (fakeStationAreaRepository) Get(context.Context, int64) (ServiceArea, error) {
	return ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, nil
}

func (fakeStationAreaRepository) ListSchedules(context.Context, int64, time.Time, time.Time) ([]ServiceAreaSchedule, error) {
	return nil, nil
}
