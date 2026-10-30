package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// ResolveAndQueue 在同一事务中固化终态、订单状态事件和必要的补派任务。
func (repository *DispatchRepository) ResolveAndQueue(ctx context.Context, resolution biz.DispatchResolution, at time.Time) (biz.DispatchRecord, error) {
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("begin dispatch resolution transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // 成功提交后事务已关闭。
	// 仅 Pending 状态可进入终态；条件更新抵御重复回调和超时扫描竞争。
	record, err := resolvePendingDispatch(ctx, tx, resolution, at)
	if errors.Is(err, ErrDispatchStateConflict) {
		return repository.findIdempotentResolution(ctx, resolution)
	}
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	if err := repository.queueResolutionEvent(ctx, tx, record, resolution, at); err != nil {
		return biz.DispatchRecord{}, err
	}
	// 接受即结束派单；拒绝/超时才需创建下一轮补派任务。
	if resolution.Status != biz.DispatchStatusAccepted {
		if err := repository.queueReassignmentJob(ctx, tx, record); err != nil {
			return biz.DispatchRecord{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("commit dispatch resolution transaction: %w", err)
	}
	return record, nil
}

func resolvePendingDispatch(ctx context.Context, tx pgx.Tx, resolution biz.DispatchResolution, at time.Time) (biz.DispatchRecord, error) {
	row := tx.QueryRow(ctx, `UPDATE public.dispatch_records SET status=$2,failure_reason=$3,responded_at=$4,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status=$5 AND ($6::BIGINT=0 OR order_id=$6) AND ($7::BIGINT=0 OR vehicle_id=$7)
		RETURNING id,dispatch_no,order_id,vehicle_id,attempt_no,dispatch_strategy,status,failure_reason,dispatched_at,responded_at`,
		resolution.DispatchID, resolution.Status, resolution.FailureReason, at, biz.DispatchStatusPending, resolution.OrderID, resolution.VehicleID)
	return scanConditionalDispatchRecord(row)
}

func (repository *DispatchRepository) queueResolutionEvent(ctx context.Context, tx pgx.Tx, record biz.DispatchRecord, resolution biz.DispatchResolution, at time.Time) error {
	// 模块 2 接收的是业务阶段事件，不直接读取模块 3 的内部状态整数。
	eventType := biz.OrderDispatchEventAssigned
	if resolution.Status == biz.DispatchStatusRejected {
		eventType = biz.OrderDispatchEventRejected
	}
	if resolution.Status == biz.DispatchStatusTimedOut {
		eventType = biz.OrderDispatchEventTimedOut
	}
	event := biz.OrderDispatchEvent{DispatchNo: record.DispatchNo, OrderID: record.OrderID, DispatchID: record.ID,
		VehicleID: record.VehicleID, Type: eventType, Reason: resolution.FailureReason, OccurredAt: at}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal resolution event: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.order_dispatch_outbox (id,dispatch_id,idempotency_key,event_type,payload,status)
		VALUES ($1,$2,$3,$4,$5,0)`, repository.data.dispatchIDGenerator.New(), record.ID, record.DispatchNo, eventType, payload)
	if err != nil {
		return fmt.Errorf("insert resolution event: %w", err)
	}
	return nil
}

func (repository *DispatchRepository) queueReassignmentJob(ctx context.Context, tx pgx.Tx, record biz.DispatchRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO public.dispatch_reassignment_jobs (id,order_id,trigger_dispatch_id,status)
		VALUES ($1,$2,$3,0)`, repository.data.dispatchIDGenerator.New(), record.OrderID, record.ID)
	if err != nil {
		return fmt.Errorf("insert reassignment job: %w", err)
	}
	return nil
}

var _ biz.AtomicDispatchResolutionRepository = (*DispatchRepository)(nil)
