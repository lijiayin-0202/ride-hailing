package service

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestServiceAreaAdminServiceMapsScheduleConflictToBadRequest(t *testing.T) {
	service := NewServiceAreaAdminService(&fakeServiceAreaAdminApplication{err: biz.ErrInvalidAreaSchedule})

	_, err := service.ReplaceServiceAreaSchedules(context.Background(), &v1.ReplaceServiceAreaSchedulesRequest{Id: 1})
	if kratoserrors.Code(err) != 400 || kratoserrors.Reason(err) != "INVALID_AREA_SCHEDULE" {
		t.Fatalf("ReplaceServiceAreaSchedules() error = %v, want 400 INVALID_AREA_SCHEDULE", err)
	}
}

func TestServiceAreaAdminServiceUpdateDoesNotMapAreaCode(t *testing.T) {
	application := &fakeServiceAreaAdminApplication{area: serviceTestArea()}
	service := NewServiceAreaAdminService(application)

	_, err := service.UpdateServiceArea(context.Background(), &v1.UpdateServiceAreaRequest{Id: 1,
		AreaName: "更新区域", CityCode: "110000", CityName: "北京市", BoundaryGeojson: `{}`})
	if err != nil {
		t.Fatalf("UpdateServiceArea() error = %v", err)
	}
	if application.update.AreaCode != "" {
		t.Fatalf("UpdateServiceArea() AreaCode = %q, want empty immutable field", application.update.AreaCode)
	}
}

func TestServiceAreaAdminServiceMapsNotFoundAndConflict(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   int
		reason string
		call   func(*ServiceAreaAdminService) error
	}{
		{name: "not found", err: biz.ErrServiceAreaNotFound, want: 404, reason: "SERVICE_AREA_NOT_FOUND",
			call: func(service *ServiceAreaAdminService) error {
				_, err := service.GetServiceArea(context.Background(), &v1.GetServiceAreaRequest{Id: 1})
				return err
			}},
		{name: "code conflict", err: biz.ErrServiceAreaCodeConflict, want: 409, reason: "SERVICE_AREA_CODE_CONFLICT",
			call: func(service *ServiceAreaAdminService) error {
				_, err := service.CreateServiceArea(context.Background(), &v1.CreateServiceAreaRequest{})
				return err
			}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call(NewServiceAreaAdminService(&fakeServiceAreaAdminApplication{err: test.err}))
			if kratoserrors.Code(err) != test.want || kratoserrors.Reason(err) != test.reason {
				t.Fatalf("error = %v, want %d %s", err, test.want, test.reason)
			}
		})
	}
}

type fakeServiceAreaAdminApplication struct {
	area   biz.ManagedServiceArea
	update biz.UpdateServiceAreaCommand
	err    error
}

func (application *fakeServiceAreaAdminApplication) Create(context.Context, biz.CreateServiceAreaCommand) (biz.ManagedServiceArea, error) {
	return application.area, application.err
}

func (application *fakeServiceAreaAdminApplication) List(context.Context, biz.ServiceAreaListQuery) ([]biz.ManagedServiceArea, int64, error) {
	return nil, 0, application.err
}

func (application *fakeServiceAreaAdminApplication) GetDetail(context.Context, int64) (biz.ManagedServiceAreaDetail, error) {
	return biz.ManagedServiceAreaDetail{ServiceArea: application.area}, application.err
}

func (application *fakeServiceAreaAdminApplication) Update(_ context.Context, command biz.UpdateServiceAreaCommand) (biz.ManagedServiceArea, error) {
	application.update = command
	return application.area, application.err
}

func (application *fakeServiceAreaAdminApplication) ChangeStatus(context.Context, int64, int16) (biz.ManagedServiceArea, error) {
	return application.area, application.err
}

func (application *fakeServiceAreaAdminApplication) ReplaceSchedules(context.Context, int64, []biz.ManagedServiceAreaSchedule) error {
	return application.err
}

func serviceTestArea() biz.ManagedServiceArea {
	return biz.ManagedServiceArea{ID: 1, AreaCode: "BJ-CORE", AreaName: "北京核心区", CityCode: "110000",
		CityName: "北京市", BoundaryGeoJSON: `{}`, Status: biz.ServiceAreaStatusOperating}
}
