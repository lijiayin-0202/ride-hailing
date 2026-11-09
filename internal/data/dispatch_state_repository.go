package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var ErrDispatchStateConflict = errors.New("派单状态已变化")

// MarkDispatched 将已创建派单尝试转为等待确认状态。
func (repository *DispatchRepository) MarkDispatched(ctx context.Context, dispatchID int64, at time.Time) (biz.DispatchRecord, error) {
	row := repository.data.pool.QueryRow(ctx, `
		UPDATE public.dispatch_records
		SET status = $2, dispatched_at = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = $4
		RETURNING id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
			status, failure_reason, dispatched_at, responded_at`,
		dispatchID, biz.DispatchStatusPending, at, biz.DispatchStatusCreated)
	return scanConditionalDispatchRecord(row)
}

// Resolve 将待确认派单尝试转为确认、拒绝或超时终态。
func (repository *DispatchRepository) Resolve(ctx context.Context, resolution biz.DispatchResolution, at time.Time) (biz.DispatchRecord, error) {
	// 条件中包含订单和车辆事实，防止错误回调确认了同一个 dispatch ID 以外的业务对象。
	row := repository.data.pool.QueryRow(ctx, `
		UPDATE public.dispatch_records
		SET status = $2, failure_reason = $3, responded_at = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = $5
			AND ($6::BIGINT = 0 OR order_id = $6::BIGINT)
			AND ($7::BIGINT = 0 OR vehicle_id = $7::BIGINT)
		RETURNING id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
			status, failure_reason, dispatched_at, responded_at`,
		resolution.DispatchID, resolution.Status, resolution.FailureReason, at, biz.DispatchStatusPending,
		resolution.OrderID, resolution.VehicleID)
	record, err := scanConditionalDispatchRecord(row)
	// 重复回调若与既有终态完全一致，则返回原记录保持客户端幂等。
	if !errors.Is(err, ErrDispatchStateConflict) {
		return record, err
	}
	return repository.findIdempotentResolution(ctx, resolution)
}

func (repository *DispatchRepository) findIdempotentResolution(ctx context.Context, resolution biz.DispatchResolution) (biz.DispatchRecord, error) {
	row := repository.data.pool.QueryRow(ctx, `
		SELECT id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
			status, failure_reason, dispatched_at, responded_at
		FROM public.dispatch_records
		WHERE id = $1 AND status = $2
			AND ($3::BIGINT = 0 OR order_id = $3::BIGINT)
			AND ($4::BIGINT = 0 OR vehicle_id = $4::BIGINT)
			AND COALESCE(failure_reason, '') = $5`,
		resolution.DispatchID, resolution.Status, resolution.OrderID, resolution.VehicleID, resolution.FailureReason)
	return scanConditionalDispatchRecord(row)
}

// ListAttemptedVehicleIDs 返回订单历史派单尝试车辆，按尝试序号排序。
func (repository *DispatchRepository) ListAttemptedVehicleIDs(ctx context.Context, orderID int64) ([]int64, error) {
	rows, err := repository.data.pool.Query(ctx, `
		SELECT vehicle_id FROM public.dispatch_records
		WHERE order_id = $1 ORDER BY attempt_no ASC, id ASC`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list attempted vehicles: %w", err)
	}
	defer rows.Close()
	return scanAttemptedVehicleIDs(rows)
}

// ListPendingBefore 返回确认窗口已到期的待确认派单尝试。
func (repository *DispatchRepository) ListPendingBefore(ctx context.Context, before time.Time) ([]biz.DispatchRecord, error) {
	rows, err := repository.data.pool.Query(ctx, `
		SELECT id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
			status, failure_reason, dispatched_at, responded_at
		FROM public.dispatch_records
		WHERE status = $1 AND dispatched_at <= $2
		ORDER BY dispatched_at ASC, id ASC`, biz.DispatchStatusPending, before)
	if err != nil {
		return nil, fmt.Errorf("list pending dispatch records: %w", err)
	}
	defer rows.Close()
	return scanPendingDispatchRecords(rows)
}

func scanConditionalDispatchRecord(row pgx.Row) (biz.DispatchRecord, error) {
	record, err := scanDispatchRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.DispatchRecord{}, ErrDispatchStateConflict
	}
	return record, err
}

func scanDispatchRecord(row pgx.Row) (biz.DispatchRecord, error) {
	var record biz.DispatchRecord
	var failureReason *string
	err := row.Scan(
		&record.ID, &record.DispatchNo, &record.OrderID, &record.VehicleID,
		&record.AttemptNo, &record.DispatchStrategy, &record.Status, &failureReason,
		&record.DispatchedAt, &record.RespondedAt,
	)
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	if failureReason != nil {
		record.FailureReason = *failureReason
	}
	return record, nil
}

func scanAttemptedVehicleIDs(rows pgx.Rows) ([]int64, error) {
	vehicles := make([]int64, 0)
	for rows.Next() {
		var vehicleID int64
		if err := rows.Scan(&vehicleID); err != nil {
			return nil, fmt.Errorf("scan attempted vehicle: %w", err)
		}
		vehicles = append(vehicles, vehicleID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempted vehicles: %w", err)
	}
	return vehicles, nil
}

func scanPendingDispatchRecords(rows pgx.Rows) ([]biz.DispatchRecord, error) {
	records := make([]biz.DispatchRecord, 0)
	for rows.Next() {
		record, err := scanDispatchRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending dispatch record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending dispatch records: %w", err)
	}
	return records, nil
}

var _ biz.AttemptedVehicleRepository = (*DispatchRepository)(nil)
var _ biz.DispatchStateRepository = (*DispatchRepository)(nil)
