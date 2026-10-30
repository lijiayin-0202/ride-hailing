package biz

import (
	"context"
	"errors"
	"testing"
)

func TestServiceAreaAdminUsecaseRejectsChangedAreaCode(t *testing.T) {
	repository := &fakeServiceAreaAdminRepository{area: ManagedServiceArea{ID: 1, AreaCode: "BJ-CORE"}}
	usecase := NewServiceAreaAdminUsecase(repository)

	_, err := usecase.Update(context.Background(), UpdateServiceAreaCommand{
		ID: 1, AreaCode: "SH-CORE", AreaName: "上海核心区", CityCode: "310000", CityName: "上海市", BoundaryGeoJSON: `{}`,
	})
	if !errors.Is(err, ErrImmutableServiceAreaCode) {
		t.Fatalf("Update() error = %v, want ErrImmutableServiceAreaCode", err)
	}
}

func TestServiceAreaAdminUsecaseRejectsInvalidManagedArea(t *testing.T) {
	usecase := NewServiceAreaAdminUsecase(&fakeServiceAreaAdminRepository{})

	_, err := usecase.Create(context.Background(), CreateServiceAreaCommand{
		AreaCode: "BJ-CORE", AreaName: "北京核心区", CityCode: "110000", CityName: "北京市", BoundaryGeoJSON: "not-json",
	})
	if !errors.Is(err, ErrInvalidManagedServiceArea) {
		t.Fatalf("Create() error = %v, want ErrInvalidManagedServiceArea", err)
	}
}

func TestServiceAreaAdminUsecaseValidatesSchedulesBeforeReplacing(t *testing.T) {
	repository := &fakeServiceAreaAdminRepository{}
	usecase := NewServiceAreaAdminUsecase(repository)

	err := usecase.ReplaceSchedules(context.Background(), 1, []ManagedServiceAreaSchedule{
		{DayOfWeek: 1, StartMinute: 540, EndMinute: 720, IsEnabled: true},
		{DayOfWeek: 1, StartMinute: 600, EndMinute: 780, IsEnabled: true},
	})
	if !errors.Is(err, ErrInvalidAreaSchedule) {
		t.Fatalf("ReplaceSchedules() error = %v, want ErrInvalidAreaSchedule", err)
	}
	if repository.replaceCalled {
		t.Fatal("ReplaceSchedules() called repository for invalid schedules")
	}
}

func TestServiceAreaAdminUsecaseAcceptsEmptySchedulesAndValidStatus(t *testing.T) {
	repository := &fakeServiceAreaAdminRepository{}
	usecase := NewServiceAreaAdminUsecase(repository)

	if err := usecase.ReplaceSchedules(context.Background(), 1, nil); err != nil {
		t.Fatalf("ReplaceSchedules() error = %v", err)
	}
	if !repository.replaceCalled || len(repository.schedules) != 0 {
		t.Fatal("ReplaceSchedules() did not pass an empty list to the repository")
	}
	if _, err := usecase.ChangeStatus(context.Background(), 1, ServiceAreaStatusOffline); err != nil {
		t.Fatalf("ChangeStatus() error = %v", err)
	}
}

type fakeServiceAreaAdminRepository struct {
	area          ManagedServiceArea
	schedules     []ManagedServiceAreaSchedule
	replaceCalled bool
}

func (repository *fakeServiceAreaAdminRepository) Create(_ context.Context, area ManagedServiceArea) (ManagedServiceArea, error) {
	return area, nil
}

func (repository *fakeServiceAreaAdminRepository) List(context.Context, ServiceAreaListQuery) ([]ManagedServiceArea, int64, error) {
	return nil, 0, nil
}

func (repository *fakeServiceAreaAdminRepository) GetDetail(context.Context, int64) (ManagedServiceAreaDetail, error) {
	return ManagedServiceAreaDetail{ServiceArea: repository.area}, nil
}

func (repository *fakeServiceAreaAdminRepository) Update(_ context.Context, area ManagedServiceArea) (ManagedServiceArea, error) {
	repository.area = area
	return area, nil
}

func (repository *fakeServiceAreaAdminRepository) ChangeStatus(_ context.Context, id int64, status int16) (ManagedServiceArea, error) {
	repository.area.ID, repository.area.Status = id, status
	return repository.area, nil
}

func (repository *fakeServiceAreaAdminRepository) ReplaceSchedules(_ context.Context, _ int64, schedules []ManagedServiceAreaSchedule) error {
	repository.replaceCalled = true
	repository.schedules = schedules
	return nil
}
