package service

import (
	"context"
	"errors"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var errEmptyStationRequest = errors.New("站点请求不能为空")

// StationApplication 描述站点服务依赖的业务用例。
type StationApplication interface {
	Create(context.Context, biz.CreateStationCommand) (biz.Station, error)
	Update(context.Context, biz.UpdateStationCommand) (biz.Station, error)
	ChangeStatus(context.Context, int64, int16) (biz.Station, error)
	List(context.Context, int64) ([]biz.Station, error)
}

// StationService 处理已鉴权的站点后台请求。
type StationService struct {
	v1.UnimplementedStationServiceServer
	usecase StationApplication
}

// NewStationService 创建站点服务。
func NewStationService(usecase StationApplication) *StationService {
	return &StationService{usecase: usecase}
}

// CreateStation 创建站点。
func (service *StationService) CreateStation(ctx context.Context, request *v1.CreateStationRequest) (*v1.StationReply, error) {
	if request == nil {
		return nil, errEmptyStationRequest
	}
	// 在边界层转换 protobuf，保证领域层不依赖传输协议生成类型。
	station, err := service.usecase.Create(ctx, createStationCommand(request))
	if err != nil {
		return nil, stationServiceError(err)
	}
	return &v1.StationReply{Station: stationReply(station)}, nil
}

// UpdateStation 更新可编辑的站点资料。
func (service *StationService) UpdateStation(ctx context.Context, request *v1.UpdateStationRequest) (*v1.StationReply, error) {
	if request == nil {
		return nil, errEmptyStationRequest
	}
	station, err := service.usecase.Update(ctx, updateStationCommand(request))
	if err != nil {
		return nil, stationServiceError(err)
	}
	return &v1.StationReply{Station: stationReply(station)}, nil
}

// ChangeStationStatus 启用或停用站点。
func (service *StationService) ChangeStationStatus(ctx context.Context, request *v1.ChangeStationStatusRequest) (*v1.StationReply, error) {
	if request == nil {
		return nil, errEmptyStationRequest
	}
	station, err := service.usecase.ChangeStatus(ctx, request.GetId(), int16(request.GetStatus()))
	if err != nil {
		return nil, stationServiceError(err)
	}
	return &v1.StationReply{Station: stationReply(station)}, nil
}

// ListStations 返回后台维护站点列表。
func (service *StationService) ListStations(ctx context.Context, request *v1.ListStationsRequest) (*v1.ListStationsReply, error) {
	if request == nil {
		return nil, errEmptyStationRequest
	}
	stations, err := service.usecase.List(ctx, request.GetServiceAreaId())
	if err != nil {
		return nil, stationServiceError(err)
	}
	return &v1.ListStationsReply{Stations: stationReplies(stations)}, nil
}

func stationServiceError(err error) error {
	if errors.Is(err, biz.ErrInvalidStation) {
		return kratoserrors.BadRequest("INVALID_STATION_REQUEST", err.Error())
	}
	return err
}

func createStationCommand(request *v1.CreateStationRequest) biz.CreateStationCommand {
	return biz.CreateStationCommand{ServiceAreaID: request.GetServiceAreaId(), StationCode: request.GetStationCode(),
		StationName: request.GetStationName(), AliasName: request.GetAliasName(), Address: request.GetAddress(),
		Longitude: request.GetLongitude(), Latitude: request.GetLatitude(), StationType: int16(request.GetStationType()),
		ServiceDirection: int16(request.GetServiceDirection()), DirectionHint: request.GetDirectionHint()}
}

func updateStationCommand(request *v1.UpdateStationRequest) biz.UpdateStationCommand {
	// 更新协议刻意不接收区域和编码；两者是站点创建后的不可变定位信息。
	return biz.UpdateStationCommand{ID: request.GetId(), StationName: request.GetStationName(), AliasName: request.GetAliasName(),
		Address: request.GetAddress(), Longitude: request.GetLongitude(), Latitude: request.GetLatitude(),
		StationType: int16(request.GetStationType()), ServiceDirection: int16(request.GetServiceDirection()),
		DirectionHint: request.GetDirectionHint()}
}

func stationReplies(stations []biz.Station) []*v1.Station {
	replies := make([]*v1.Station, 0, len(stations))
	for _, station := range stations {
		replies = append(replies, stationReply(station))
	}
	return replies
}

func stationReply(station biz.Station) *v1.Station {
	return &v1.Station{Id: station.ID, ServiceAreaId: station.ServiceAreaID, StationCode: station.StationCode,
		StationName: station.StationName, AliasName: station.AliasName, Address: station.Address,
		Longitude: station.Longitude, Latitude: station.Latitude, StationType: int32(station.StationType),
		ServiceDirection: int32(station.ServiceDirection), DirectionHint: station.DirectionHint, Status: int32(station.Status)}
}

var _ v1.StationServiceServer = (*StationService)(nil)
