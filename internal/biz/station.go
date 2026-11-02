package biz

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	StationTypeNormal      int16 = 1
	StationTypeRecommended int16 = 2
	StationTypeVehicleBase int16 = 3

	StationDirectionPickup  int16 = 1
	StationDirectionDropoff int16 = 2
	StationDirectionBoth    int16 = 3

	StationStatusDisabled int16 = 0
	StationStatusEnabled  int16 = 1
)

var (
	ErrInvalidStation        = errors.New("站点数据无效")
	ErrImmutableStationField = errors.New("站点不可变字段不能修改")
	stationCodePattern       = regexp.MustCompile("^[A-Za-z0-9_-]{1,40}$")
)

// Station 是站点维护和选点所需的领域事实。
type Station struct {
	ID               int64
	ServiceAreaID    int64
	StationCode      string
	StationName      string
	AliasName        string
	Address          string
	Longitude        float64
	Latitude         float64
	StationType      int16
	ServiceDirection int16
	DirectionHint    string
	Status           int16
}

// CreateStationCommand 是创建站点的可写字段。
type CreateStationCommand struct {
	ServiceAreaID    int64
	StationCode      string
	StationName      string
	AliasName        string
	Address          string
	Longitude        float64
	Latitude         float64
	StationType      int16
	ServiceDirection int16
	DirectionHint    string
}

// UpdateStationCommand 是更新站点的可写字段；编码与区域仅用于拒绝越权变更。
type UpdateStationCommand struct {
	ID               int64
	ServiceAreaID    int64
	StationCode      string
	StationName      string
	AliasName        string
	Address          string
	Longitude        float64
	Latitude         float64
	StationType      int16
	ServiceDirection int16
	DirectionHint    string
}

// StationRepository 定义站点持久化端口。
type StationRepository interface {
	Create(context.Context, Station) (Station, error)
	Get(context.Context, int64) (Station, error)
	Update(context.Context, Station) (Station, error)
	ChangeStatus(context.Context, int64, int16) (Station, error)
	List(context.Context, int64) ([]Station, error)
	ListAvailable(context.Context, int64, int16) ([]Station, error)
}

// StationUsecase 处理站点维护和可用上下车站点判定。
type StationUsecase struct {
	stations StationRepository
	areas    ServiceAreaRepository
}

// NewStationUsecase 创建站点用例。
func NewStationUsecase(stations StationRepository, areas ServiceAreaRepository) *StationUsecase {
	return &StationUsecase{stations: stations, areas: areas}
}

// Create 校验输入和所属区域后创建站点。
func (usecase *StationUsecase) Create(ctx context.Context, command CreateStationCommand) (Station, error) {
	station := stationFromCreate(command)
	if err := validateStation(station); err != nil {
		return Station{}, err
	}
	if _, err := usecase.areas.Get(ctx, station.ServiceAreaID); err != nil {
		return Station{}, err
	}
	return usecase.stations.Create(ctx, station)
}

// Update 仅允许更新可编辑字段。
func (usecase *StationUsecase) Update(ctx context.Context, command UpdateStationCommand) (Station, error) {
	if command.ID <= 0 {
		return Station{}, ErrInvalidStation
	}
	current, err := usecase.stations.Get(ctx, command.ID)
	if err != nil {
		return Station{}, err
	}
	// 区域和站点编码参与跨模块定位，创建后不可改，避免历史订单无法回溯。
	if changedImmutableField(current, command) {
		return Station{}, ErrImmutableStationField
	}
	station := stationFromUpdate(current, command)
	if err := validateStation(station); err != nil {
		return Station{}, err
	}
	return usecase.stations.Update(ctx, station)
}

// ChangeStatus 启用或停用已有站点。
func (usecase *StationUsecase) ChangeStatus(ctx context.Context, id int64, status int16) (Station, error) {
	if id <= 0 || !isValidStationStatus(status) {
		return Station{}, ErrInvalidStation
	}
	return usecase.stations.ChangeStatus(ctx, id, status)
}

// List 返回后台维护站点；areaID 为零时由仓储返回全部站点。
func (usecase *StationUsecase) List(ctx context.Context, areaID int64) ([]Station, error) {
	if areaID < 0 {
		return nil, ErrInvalidStation
	}
	return usecase.stations.List(ctx, areaID)
}

// ListAvailable 返回在指定区域可用于上车或下车的站点。
func (usecase *StationUsecase) ListAvailable(ctx context.Context, areaID int64, direction int16) ([]Station, error) {
	if areaID <= 0 || !isValidStationDirection(direction) {
		return nil, ErrInvalidStation
	}
	area, err := usecase.areas.Get(ctx, areaID)
	if err != nil {
		return nil, err
	}
	// 区域未运营时不暴露上下车站点，即使站点自身仍为启用状态。
	if area.Status != ServiceAreaStatusOperating {
		return []Station{}, nil
	}
	stations, err := usecase.stations.ListAvailable(ctx, areaID, direction)
	if err != nil {
		return nil, err
	}
	return filterAvailableStations(stations, direction), nil
}

func stationFromCreate(command CreateStationCommand) Station {
	return Station{ServiceAreaID: command.ServiceAreaID, StationCode: command.StationCode,
		StationName: command.StationName, AliasName: command.AliasName, Address: command.Address,
		Longitude: command.Longitude, Latitude: command.Latitude, StationType: command.StationType,
		ServiceDirection: command.ServiceDirection, DirectionHint: command.DirectionHint, Status: StationStatusEnabled}
}

func stationFromUpdate(current Station, command UpdateStationCommand) Station {
	station := stationFromCreate(CreateStationCommand{ServiceAreaID: current.ServiceAreaID,
		StationCode: current.StationCode, StationName: command.StationName, AliasName: command.AliasName,
		Address: command.Address, Longitude: command.Longitude, Latitude: command.Latitude,
		StationType: command.StationType, ServiceDirection: command.ServiceDirection, DirectionHint: command.DirectionHint})
	station.ID, station.Status = current.ID, current.Status
	return station
}

func changedImmutableField(current Station, command UpdateStationCommand) bool {
	return command.StationCode != "" && command.StationCode != current.StationCode ||
		command.ServiceAreaID != 0 && command.ServiceAreaID != current.ServiceAreaID
}

func validateStation(station Station) error {
	if station.ServiceAreaID <= 0 || !stationCodePattern.MatchString(station.StationCode) ||
		!validRequiredText(station.StationName, 120) || !validRequiredText(station.Address, 300) ||
		!validOptionalText(station.AliasName, 120) || !validOptionalText(station.DirectionHint, 300) ||
		station.Longitude < 73 || station.Longitude > 135.1 || station.Latitude < 3.5 || station.Latitude > 53.6 ||
		!isValidStationType(station.StationType) || !isValidStationDirection(station.ServiceDirection) ||
		!isValidStationStatus(station.Status) {
		return ErrInvalidStation
	}
	return nil
}

func validRequiredText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}

func validOptionalText(value string, limit int) bool { return utf8.RuneCountInString(value) <= limit }

func isValidStationType(value int16) bool {
	return value >= StationTypeNormal && value <= StationTypeVehicleBase
}

func isValidStationDirection(value int16) bool {
	return value >= StationDirectionPickup && value <= StationDirectionBoth
}

func isValidStationStatus(value int16) bool {
	return value == StationStatusDisabled || value == StationStatusEnabled
}

func filterAvailableStations(stations []Station, direction int16) []Station {
	available := make([]Station, 0, len(stations))
	for _, station := range stations {
		// 车辆基地只能用于调度保障，不能作为乘客上下车点。
		if station.Status == StationStatusEnabled && station.StationType != StationTypeVehicleBase &&
			(station.ServiceDirection == direction || station.ServiceDirection == StationDirectionBoth) {
			available = append(available, station)
		}
	}
	return available
}
