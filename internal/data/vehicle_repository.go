package data

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// VehicleRepository 从 PostgreSQL 读取车辆档案和最新状态事实。
type VehicleRepository struct {
	data *Data
}

// NewVehicleRepository 创建车辆状态聚合仓储。
func NewVehicleRepository(data *Data) *VehicleRepository {
	return &VehicleRepository{data: data}
}

// ListSnapshots 返回一个运营区域内每辆车的档案与最新状态。
func (repository *VehicleRepository) ListSnapshots(ctx context.Context, serviceAreaID int64) ([]biz.VehicleSnapshot, error) {
	// 返回无状态日志的车辆档案，调用方可明确区分“无最新状态”与离线状态。
	rows, err := repository.data.pool.Query(ctx, vehicleSnapshotSQL, serviceAreaID)
	if err != nil {
		return nil, fmt.Errorf("list vehicle snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]biz.VehicleSnapshot, 0)
	for rows.Next() {
		snapshot, err := scanVehicleSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vehicle snapshots: %w", err)
	}
	return snapshots, nil
}

func scanVehicleSnapshot(row pgx.Row) (biz.VehicleSnapshot, error) {
	var snapshot biz.VehicleSnapshot
	var runtime vehicleRuntimeColumns
	err := row.Scan(
		&snapshot.Vehicle.ID, &snapshot.Vehicle.VehicleNo, &snapshot.Vehicle.VIN,
		&snapshot.Vehicle.PlateNo, &snapshot.Vehicle.VehicleModel, &snapshot.Vehicle.SeatCount,
		&snapshot.Vehicle.ServiceAreaID, &snapshot.Vehicle.SoftwareVer, &runtime.online, &runtime.operation,
		&runtime.battery, &runtime.longitude, &runtime.latitude, &runtime.faultCode, &runtime.accepting, &runtime.reportedAt,
	)
	if err != nil {
		return biz.VehicleSnapshot{}, fmt.Errorf("scan vehicle snapshot: %w", err)
	}
	return newVehicleSnapshot(snapshot, runtime), nil
}

type vehicleRuntimeColumns struct {
	online, operation            *int16
	battery, longitude, latitude *float64
	faultCode                    *string
	accepting                    *bool
	reportedAt                   *time.Time
}

func newVehicleSnapshot(snapshot biz.VehicleSnapshot, runtime vehicleRuntimeColumns) biz.VehicleSnapshot {
	if runtime.reportedAt == nil {
		return snapshot
	}
	status := &biz.VehicleRuntimeStatus{
		OnlineStatus:        *runtime.online,
		OperationStatus:     *runtime.operation,
		BatteryPercent:      runtime.battery,
		Longitude:           runtime.longitude,
		Latitude:            runtime.latitude,
		IsAcceptingDispatch: *runtime.accepting,
		ReportedAt:          *runtime.reportedAt,
	}
	if runtime.faultCode != nil {
		status.FaultCode = *runtime.faultCode
	}
	snapshot.RuntimeStatus = status
	return snapshot
}

const vehicleSnapshotSQL = `
	SELECT vehicle.id, vehicle.vehicle_no, vehicle.vin, vehicle.plate_no,
		vehicle.vehicle_model, vehicle.seat_count, vehicle.service_area_id,
		vehicle.software_version, latest.online_status, latest.operation_status,
		latest.battery_percent, latest.current_longitude, latest.current_latitude,
		latest.fault_code, latest.is_accepting_dispatch, latest.reported_at
	FROM public.vehicles AS vehicle
	-- LEFT JOIN 保留没有上报记录的车辆；LATERAL 确保每辆车只关联一条最新事实。
	LEFT JOIN LATERAL (
		SELECT online_status, operation_status, battery_percent, current_longitude,
			current_latitude, fault_code, is_accepting_dispatch, reported_at
		FROM public.vehicle_status_logs
		WHERE vehicle_id = vehicle.id
		ORDER BY reported_at DESC, id DESC
		LIMIT 1
	) AS latest ON TRUE
	WHERE vehicle.service_area_id = $1
	ORDER BY vehicle.id ASC`

var _ biz.VehicleRepository = (*VehicleRepository)(nil)
