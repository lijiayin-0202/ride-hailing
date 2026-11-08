package service

import (
	"context"
	"errors"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var errEmptyServiceAreaRequest = errors.New("运营区域请求不能为空")

// ServiceAreaAdminApplication 描述运营区域后台服务依赖的领域用例。
type ServiceAreaAdminApplication interface {
	Create(context.Context, biz.CreateServiceAreaCommand) (biz.ManagedServiceArea, error)
	List(context.Context, biz.ServiceAreaListQuery) ([]biz.ManagedServiceArea, int64, error)
	GetDetail(context.Context, int64) (biz.ManagedServiceAreaDetail, error)
	Update(context.Context, biz.UpdateServiceAreaCommand) (biz.ManagedServiceArea, error)
	ChangeStatus(context.Context, int64, int16) (biz.ManagedServiceArea, error)
	ReplaceSchedules(context.Context, int64, []biz.ManagedServiceAreaSchedule) error
}

// ServiceAreaAdminService 提供后台运营区域资料和服务时段管理接口。
type ServiceAreaAdminService struct {
	v1.UnimplementedServiceAreaAdminServiceServer
	usecase ServiceAreaAdminApplication
}

// NewServiceAreaAdminService 创建运营区域后台服务。
func NewServiceAreaAdminService(usecase ServiceAreaAdminApplication) *ServiceAreaAdminService {
	return &ServiceAreaAdminService{usecase: usecase}
}

func (service *ServiceAreaAdminService) CreateServiceArea(ctx context.Context, request *v1.CreateServiceAreaRequest) (*v1.ServiceAreaReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	area, err := service.usecase.Create(ctx, createServiceAreaCommand(request))
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return &v1.ServiceAreaReply{ServiceArea: serviceAreaReply(area)}, nil
}

func (service *ServiceAreaAdminService) ListServiceAreas(ctx context.Context, request *v1.ListServiceAreasRequest) (*v1.ListServiceAreasReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	areas, total, err := service.usecase.List(ctx, serviceAreaListQuery(request))
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return &v1.ListServiceAreasReply{ServiceAreas: serviceAreaReplies(areas), Total: total}, nil
}

func (service *ServiceAreaAdminService) GetServiceArea(ctx context.Context, request *v1.GetServiceAreaRequest) (*v1.ServiceAreaDetailReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	detail, err := service.usecase.GetDetail(ctx, request.GetId())
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return serviceAreaDetailReply(detail), nil
}

func (service *ServiceAreaAdminService) UpdateServiceArea(ctx context.Context, request *v1.UpdateServiceAreaRequest) (*v1.ServiceAreaReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	area, err := service.usecase.Update(ctx, updateServiceAreaCommand(request))
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return &v1.ServiceAreaReply{ServiceArea: serviceAreaReply(area)}, nil
}

func (service *ServiceAreaAdminService) ChangeServiceAreaStatus(ctx context.Context, request *v1.ChangeServiceAreaStatusRequest) (*v1.ServiceAreaReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	area, err := service.usecase.ChangeStatus(ctx, request.GetId(), int16(request.GetStatus()))
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return &v1.ServiceAreaReply{ServiceArea: serviceAreaReply(area)}, nil
}

func (service *ServiceAreaAdminService) ReplaceServiceAreaSchedules(ctx context.Context, request *v1.ReplaceServiceAreaSchedulesRequest) (*v1.ServiceAreaDetailReply, error) {
	if request == nil {
		return nil, serviceAreaRequestError(errEmptyServiceAreaRequest)
	}
	if err := service.usecase.ReplaceSchedules(ctx, request.GetId(), managedServiceAreaSchedules(request.GetSchedules())); err != nil {
		return nil, serviceAreaRequestError(err)
	}
	detail, err := service.usecase.GetDetail(ctx, request.GetId())
	if err != nil {
		return nil, serviceAreaRequestError(err)
	}
	return serviceAreaDetailReply(detail), nil
}

func createServiceAreaCommand(request *v1.CreateServiceAreaRequest) biz.CreateServiceAreaCommand {
	return biz.CreateServiceAreaCommand{AreaCode: request.GetAreaCode(), AreaName: request.GetAreaName(), CityCode: request.GetCityCode(),
		CityName: request.GetCityName(), BoundaryGeoJSON: request.GetBoundaryGeojson(), ServiceNotice: request.GetServiceNotice(), Status: int16(request.GetStatus())}
}

func updateServiceAreaCommand(request *v1.UpdateServiceAreaRequest) biz.UpdateServiceAreaCommand {
	return biz.UpdateServiceAreaCommand{ID: request.GetId(), AreaName: request.GetAreaName(), CityCode: request.GetCityCode(),
		CityName: request.GetCityName(), BoundaryGeoJSON: request.GetBoundaryGeojson(), ServiceNotice: request.GetServiceNotice()}
}

func serviceAreaListQuery(request *v1.ListServiceAreasRequest) biz.ServiceAreaListQuery {
	query := biz.ServiceAreaListQuery{Page: int(request.GetPage()), PageSize: int(request.GetPageSize()), CityCode: request.GetCityCode()}
	if request.Status != nil {
		status := int16(request.GetStatus())
		query.Status = &status
	}
	return query
}

func managedServiceAreaSchedules(schedules []*v1.ServiceAreaSchedule) []biz.ManagedServiceAreaSchedule {
	converted := make([]biz.ManagedServiceAreaSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		if schedule == nil {
			converted = append(converted, biz.ManagedServiceAreaSchedule{DayOfWeek: -1})
			continue
		}
		converted = append(converted, biz.ManagedServiceAreaSchedule{DayOfWeek: int(schedule.GetDayOfWeek()),
			SpecialDate: schedule.GetSpecialDate(), StartMinute: int(schedule.GetStartMinute()), EndMinute: int(schedule.GetEndMinute()),
			IsEnabled: schedule.GetIsEnabled(), Notice: schedule.GetNotice()})
	}
	return converted
}

func serviceAreaDetailReply(detail biz.ManagedServiceAreaDetail) *v1.ServiceAreaDetailReply {
	return &v1.ServiceAreaDetailReply{ServiceArea: serviceAreaReply(detail.ServiceArea), Schedules: serviceAreaScheduleReplies(detail.Schedules)}
}

func serviceAreaReplies(areas []biz.ManagedServiceArea) []*v1.ServiceArea {
	replies := make([]*v1.ServiceArea, 0, len(areas))
	for _, area := range areas {
		replies = append(replies, serviceAreaReply(area))
	}
	return replies
}

func serviceAreaReply(area biz.ManagedServiceArea) *v1.ServiceArea {
	return &v1.ServiceArea{Id: area.ID, AreaCode: area.AreaCode, AreaName: area.AreaName, CityCode: area.CityCode,
		CityName: area.CityName, BoundaryGeojson: area.BoundaryGeoJSON, ServiceNotice: area.ServiceNotice, Status: v1.ServiceAreaStatus(area.Status)}
}

func serviceAreaScheduleReplies(schedules []biz.ManagedServiceAreaSchedule) []*v1.ServiceAreaSchedule {
	replies := make([]*v1.ServiceAreaSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		replies = append(replies, &v1.ServiceAreaSchedule{DayOfWeek: int32(schedule.DayOfWeek), SpecialDate: schedule.SpecialDate,
			StartMinute: int32(schedule.StartMinute), EndMinute: int32(schedule.EndMinute), IsEnabled: schedule.IsEnabled, Notice: schedule.Notice})
	}
	return replies
}

func serviceAreaRequestError(err error) error {
	switch {
	case errors.Is(err, biz.ErrInvalidAreaSchedule):
		return kratoserrors.BadRequest("INVALID_AREA_SCHEDULE", err.Error())
	case errors.Is(err, biz.ErrServiceAreaNotFound):
		return kratoserrors.NotFound("SERVICE_AREA_NOT_FOUND", err.Error())
	case errors.Is(err, biz.ErrServiceAreaCodeConflict):
		return kratoserrors.Conflict("SERVICE_AREA_CODE_CONFLICT", err.Error())
	case errors.Is(err, errEmptyServiceAreaRequest), errors.Is(err, biz.ErrInvalidManagedServiceArea), errors.Is(err, biz.ErrImmutableServiceAreaCode):
		return kratoserrors.BadRequest("INVALID_SERVICE_AREA_REQUEST", err.Error())
	default:
		return err
	}
}

var _ v1.ServiceAreaAdminServiceServer = (*ServiceAreaAdminService)(nil)
