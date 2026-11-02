package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var ErrDispatchAttemptConflict = errors.New("派单尝试记录冲突")

// DispatchRepository 通过 PostgreSQL 保存派单记录，并读取车辆状态快照。
type DispatchRepository struct {
	data *Data
}

// NewDispatchRepository 创建派单数据仓储。
func NewDispatchRepository(data *Data) *DispatchRepository {
	return &DispatchRepository{data: data}
}

// CountAttempts 返回一个订单已有的派单尝试数。
func (repository *DispatchRepository) CountAttempts(ctx context.Context, orderID int64) (int, error) {
	var count int
	err := repository.data.pool.QueryRow(ctx,
		"SELECT count(*) FROM public.dispatch_records WHERE order_id = $1", orderID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count dispatch attempts: %w", err)
	}
	return count, nil
}

// ListEligible 返回同区域、最新状态在线空闲、允许接单且无故障的候选车辆。
func (repository *DispatchRepository) ListEligible(ctx context.Context, serviceAreaID int64, excludedVehicleIDs []int64) ([]biz.CandidateVehicle, error) {
	// LATERAL 子查询按上报时间和 ID 取唯一最新状态，历史在线记录不会误使车辆可派。
	rows, err := repository.data.pool.Query(ctx, `
		SELECT vehicle.id
		FROM public.vehicles AS vehicle
		JOIN LATERAL (
			SELECT online_status, operation_status, fault_code, is_accepting_dispatch
			FROM public.vehicle_status_logs
			WHERE vehicle_id = vehicle.id
			ORDER BY reported_at DESC, id DESC
			LIMIT 1
		) AS latest_status ON TRUE
		WHERE vehicle.service_area_id = $1
			AND latest_status.online_status = 1
			AND latest_status.operation_status = 1
			AND latest_status.is_accepting_dispatch
			AND (latest_status.fault_code IS NULL OR btrim(latest_status.fault_code) = '')
			-- 历史派过的车辆不能在同一订单的补派中再次入选。
			AND (
				COALESCE(cardinality($2::BIGINT[]), 0) = 0
				OR NOT (vehicle.id = ANY($2::BIGINT[]))
			)
		ORDER BY vehicle.id ASC`, serviceAreaID, excludedVehicleIDs)
	if err != nil {
		return nil, fmt.Errorf("list eligible vehicles: %w", err)
	}
	defer rows.Close()

	vehicles := make([]biz.CandidateVehicle, 0)
	for rows.Next() {
		var vehicle biz.CandidateVehicle
		if err := rows.Scan(&vehicle.ID); err != nil {
			return nil, fmt.Errorf("scan eligible vehicle: %w", err)
		}
		vehicles = append(vehicles, vehicle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate eligible vehicles: %w", err)
	}
	return vehicles, nil
}

// Create 新建处于已创建状态的派单尝试记录。
func (repository *DispatchRepository) Create(ctx context.Context, record biz.DispatchRecord) error {
	_, err := repository.data.pool.Exec(ctx, `
		INSERT INTO public.dispatch_records
			(id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy, status, dispatched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		record.ID, record.DispatchNo, record.OrderID, record.VehicleID, record.AttemptNo,
		record.DispatchStrategy, record.Status, record.DispatchedAt)
	if err == nil {
		return nil
	}

	var postgresError *pgconn.PgError
	// 唯一约束冲突留给上层按幂等语义回读，避免把并发请求误报为基础设施故障。
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrDispatchAttemptConflict
	}
	return fmt.Errorf("create dispatch attempt: %w", err)
}

var _ biz.DispatchRecordRepository = (*DispatchRepository)(nil)
var _ biz.CandidateVehicleRepository = (*DispatchRepository)(nil)
