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

const (
	reassignmentPending int16 = iota
	reassignmentProcessing
	reassignmentCompleted
)

type reassignmentJob struct {
	ID                int64
	OrderID           int64
	TriggerDispatchID int64
}

// ReassignmentApplication 是补派工作者调用自动派单用例的端口。
type ReassignmentApplication interface {
	Assign(context.Context, int64) (biz.AssignDispatchResult, error)
}

// ProcessReassignmentOnce 处理一条任务；可补派时创建下一次派单，否则安全结束任务。
func (repository *DispatchRepository) ProcessReassignmentOnce(ctx context.Context, application ReassignmentApplication, now time.Time, lease time.Duration) (bool, error) {
	job, found, err := repository.claimReassignmentJob(ctx, now, lease)
	if err != nil || !found {
		return found, err
	}
	// 每次补派均重新读取模块 2 订单快照，避免使用过期取消/可调度状态。
	_, err = application.Assign(ctx, job.OrderID)
	if err == nil {
		return true, repository.completeReassignmentJob(ctx, job.ID)
	}
	// 这些错误代表业务上已无继续重试价值，需结束任务并回写 EXHAUSTED。
	if errors.Is(err, biz.ErrOrderNotSchedulable) || errors.Is(err, biz.ErrNoEligibleVehicle) || errors.Is(err, biz.ErrDispatchAttemptLimitReached) {
		return true, repository.completeReassignmentWithExhausted(ctx, job)
	}
	return true, repository.retryReassignmentJob(ctx, job.ID, now, err)
}

func (repository *DispatchRepository) claimReassignmentJob(ctx context.Context, now time.Time, lease time.Duration) (reassignmentJob, bool, error) {
	row := repository.data.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM public.dispatch_reassignment_jobs
		WHERE (status=$1 AND next_attempt_at <= $2) OR (status=$3 AND lease_expires_at <= $2)
		ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
	) UPDATE public.dispatch_reassignment_jobs job SET status=$3,lease_expires_at=$4,updated_at=CURRENT_TIMESTAMP
	FROM candidate WHERE job.id=candidate.id RETURNING job.id,job.order_id,job.trigger_dispatch_id`, reassignmentPending, now, reassignmentProcessing, now.Add(lease))
	var job reassignmentJob
	err := row.Scan(&job.ID, &job.OrderID, &job.TriggerDispatchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return reassignmentJob{}, false, nil
	}
	if err != nil {
		return reassignmentJob{}, false, fmt.Errorf("claim reassignment job: %w", err)
	}
	return job, true, nil
}

func (repository *DispatchRepository) completeReassignmentWithExhausted(ctx context.Context, job reassignmentJob) error {
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin exhausted reassignment transaction: %w", err)
	}
	// EXHAUSTED 与任务完成必须原子提交，避免任务已完成却没有订单终态事件。
	defer tx.Rollback(ctx) //nolint:errcheck // 成功提交后事务已关闭。
	row := tx.QueryRow(ctx, `SELECT id,dispatch_no,order_id,vehicle_id,attempt_no,dispatch_strategy,status,failure_reason,dispatched_at,responded_at
		FROM public.dispatch_records WHERE id=$1`, job.TriggerDispatchID)
	record, err := scanDispatchRecord(row)
	if err != nil {
		return fmt.Errorf("read exhausted dispatch record: %w", err)
	}
	result, err := tx.Exec(ctx, `UPDATE public.dispatch_reassignment_jobs SET status=$2,lease_expires_at=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status=$3`, job.ID, reassignmentCompleted, reassignmentProcessing)
	if err != nil || result.RowsAffected() != 1 {
		return fmt.Errorf("complete exhausted reassignment job: %w", err)
	}
	event := biz.OrderDispatchEvent{DispatchNo: record.DispatchNo, OrderID: record.OrderID, DispatchID: record.ID,
		VehicleID: record.VehicleID, Type: biz.OrderDispatchEventExhausted, OccurredAt: time.Now().UTC()}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal exhausted event: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.order_dispatch_outbox (id,dispatch_id,idempotency_key,event_type,payload,status)
		VALUES ($1,$2,$3,$4,$5,0)`, repository.data.dispatchIDGenerator.New(), record.ID, record.DispatchNo, event.Type, payload)
	if err != nil {
		return fmt.Errorf("insert exhausted event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit exhausted reassignment transaction: %w", err)
	}
	return nil
}

func (repository *DispatchRepository) completeReassignmentJob(ctx context.Context, id int64) error {
	result, err := repository.data.pool.Exec(ctx, `UPDATE public.dispatch_reassignment_jobs
		SET status=$2,lease_expires_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status=$3`, id, reassignmentCompleted, reassignmentProcessing)
	if err != nil {
		return fmt.Errorf("complete reassignment job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("complete reassignment job: state conflict")
	}
	return nil
}

func (repository *DispatchRepository) retryReassignmentJob(ctx context.Context, id int64, now time.Time, cause error) error {
	result, err := repository.data.pool.Exec(ctx, `UPDATE public.dispatch_reassignment_jobs
		SET status=$2,attempt_count=attempt_count+1,next_attempt_at=$3,lease_expires_at=NULL,last_error=$4,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status=$5`, id, reassignmentPending, now.Add(time.Second), truncateOutboxError(cause), reassignmentProcessing)
	if err != nil {
		return fmt.Errorf("retry reassignment job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("retry reassignment job: state conflict")
	}
	return nil
}
