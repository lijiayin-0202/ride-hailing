package biz

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	VehicleOffline int16 = 0
	VehicleOnline  int16 = 1

	VehicleOperationUnknown     int16 = 0
	VehicleOperationIdle        int16 = 1
	VehicleOperationPickup      int16 = 2
	VehicleOperationPassenger   int16 = 3
	VehicleOperationCharging    int16 = 4
	VehicleOperationMaintenance int16 = 5
)

var ErrInvalidVehicleServiceArea = errors.New("车辆运营区域无效")

// Vehicle 表示稳定的车辆档案。
type Vehicle struct {
	ID            int64
	VehicleNo     string
	VIN           string
	PlateNo       string
	VehicleModel  string
	SeatCount     int
	ServiceAreaID int64
	SoftwareVer   string
}

// VehicleRuntimeStatus 表示一辆车最新的模块 4 事实上报状态。
type VehicleRuntimeStatus struct {
	OnlineStatus        int16
	OperationStatus     int16
	BatteryPercent      *float64
	Longitude           *float64
	Latitude            *float64
	FaultCode           string
	IsAcceptingDispatch bool
	ReportedAt          time.Time
}

// VehicleSnapshot 聚合档案和可选的最新运行状态。
type VehicleSnapshot struct {
	Vehicle       Vehicle
	RuntimeStatus *VehicleRuntimeStatus
}

// VehicleRepository 定义车辆档案和最新状态的只读端口。
type VehicleRepository interface {
	ListSnapshots(context.Context, int64) ([]VehicleSnapshot, error)
}

// VehicleUsecase 查询车辆档案与运行状态聚合。
type VehicleUsecase struct {
	repository VehicleRepository
}

// NewVehicleUsecase 创建车辆聚合用例。
func NewVehicleUsecase(repository VehicleRepository) *VehicleUsecase {
	return &VehicleUsecase{repository: repository}
}

// ListSnapshots 按运营区域读取车辆及各自最新状态。
func (usecase *VehicleUsecase) ListSnapshots(ctx context.Context, serviceAreaID int64) ([]VehicleSnapshot, error) {
	if serviceAreaID <= 0 {
		return nil, ErrInvalidVehicleServiceArea
	}
	return usecase.repository.ListSnapshots(ctx, serviceAreaID)
}

// IsDispatchEligible 判断最新状态是否可作为新派单候选。
func IsDispatchEligible(status *VehicleRuntimeStatus) bool {
	// 仅最新状态同时满足在线、空闲、无故障且允许接单时，才能避免向不可服务车辆派单。
	return status != nil &&
		status.OnlineStatus == VehicleOnline &&
		status.OperationStatus == VehicleOperationIdle &&
		status.IsAcceptingDispatch &&
		strings.TrimSpace(status.FaultCode) == ""
}
