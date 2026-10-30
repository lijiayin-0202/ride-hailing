package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// Queue 将状态流转和派单指令快照放在同一 PostgreSQL 事务中。
func (repository *DispatchRepository) Queue(ctx context.Context, instruction biz.VehicleDispatchInstruction) (biz.DispatchRecord, error) {
	if instruction.Dispatch.ID <= 0 || instruction.Dispatch.DispatchNo == "" || repository.data.dispatchIDGenerator == nil {
		return biz.DispatchRecord{}, fmt.Errorf("invalid dispatch outbox input")
	}
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("begin dispatch outbox transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // 成功提交后 Rollback 返回 pgx.ErrTxClosed。

	// 记录变为 Pending 与 Outbox 入队必须同事务，否则可能出现车辆未收到却已等待确认的孤儿派单。
	record, err := queuePendingDispatch(ctx, tx, instruction.Dispatch.ID)
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	if !sameDispatchFacts(record, instruction.Dispatch) {
		return biz.DispatchRecord{}, fmt.Errorf("dispatch outbox snapshot does not match persisted dispatch")
	}
	instruction.Dispatch = record
	payload, err := json.Marshal(instruction)
	if err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("marshal dispatch outbox payload: %w", err)
	}
	if err := insertDispatchOutbox(ctx, tx, repository.data.dispatchIDGenerator.New(), record, payload); err != nil {
		return biz.DispatchRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("commit dispatch outbox transaction: %w", err)
	}
	return record, nil
}

func sameDispatchFacts(record, snapshot biz.DispatchRecord) bool {
	return record.ID == snapshot.ID &&
		record.DispatchNo == snapshot.DispatchNo &&
		record.OrderID == snapshot.OrderID &&
		record.VehicleID == snapshot.VehicleID &&
		record.AttemptNo == snapshot.AttemptNo &&
		record.DispatchStrategy == snapshot.DispatchStrategy
}

func queuePendingDispatch(ctx context.Context, tx pgx.Tx, dispatchID int64) (biz.DispatchRecord, error) {
	// 条件更新只允许 Created 进入 Pending，重复入队会形成状态冲突而非重复下发。
	row := tx.QueryRow(ctx, `
		UPDATE public.dispatch_records
		SET status = $2, dispatched_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = $3
		RETURNING id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
			status, failure_reason, dispatched_at, responded_at`,
		dispatchID, biz.DispatchStatusPending, biz.DispatchStatusCreated)
	record, err := scanConditionalDispatchRecord(row)
	if err != nil {
		return biz.DispatchRecord{}, fmt.Errorf("mark dispatch pending for outbox: %w", err)
	}
	return record, nil
}

func insertDispatchOutbox(ctx context.Context, tx pgx.Tx, outboxID int64, record biz.DispatchRecord, payload []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO public.dispatch_outbox (id, dispatch_id, idempotency_key, payload, status)
		VALUES ($1, $2, $3, $4, $5)`,
		outboxID, record.ID, record.DispatchNo, payload, 0)
	if err != nil {
		return fmt.Errorf("insert dispatch outbox: %w", err)
	}
	return nil
}
