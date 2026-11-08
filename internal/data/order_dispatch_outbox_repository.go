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
	orderDispatchOutboxPending int16 = iota
	orderDispatchOutboxSending
	orderDispatchOutboxDelivered
)

type orderDispatchOutboxEntry struct {
	ID           int64
	Payload      []byte
	AttemptCount int
}

// EnqueueOrderDispatchEvent 持久化模块 2 状态同步事件。
func (repository *DispatchRepository) EnqueueOrderDispatchEvent(ctx context.Context, event biz.OrderDispatchEvent) error {
	if repository.data.dispatchIDGenerator == nil || event.DispatchID <= 0 || event.DispatchNo == "" {
		return fmt.Errorf("invalid order dispatch event")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal order dispatch event: %w", err)
	}
	_, err = repository.data.pool.Exec(ctx, `INSERT INTO public.order_dispatch_outbox
		(id,dispatch_id,idempotency_key,event_type,payload,status) VALUES ($1,$2,$3,$4,$5,0)`,
		repository.data.dispatchIDGenerator.New(), event.DispatchID, event.DispatchNo, event.Type, payload)
	if err != nil {
		return fmt.Errorf("enqueue order dispatch event: %w", err)
	}
	return nil
}

// ProcessOrderDispatchOutboxOnce 领取一条到期事件并向模块 2 投递。
func (repository *DispatchRepository) ProcessOrderDispatchOutboxOnce(ctx context.Context, gateway biz.OrderDispatchSyncGateway, now time.Time, lease time.Duration) (bool, error) {
	// 先以租约独占消息，再执行外部 gRPC；外部失败不会回滚已持久化的业务事实。
	entry, found, err := repository.claimOrderDispatchOutbox(ctx, now, lease)
	if err != nil || !found {
		return found, err
	}
	var event biz.OrderDispatchEvent
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		return true, repository.retryOrderDispatchOutbox(ctx, entry, now, fmt.Errorf("decode order dispatch event: %w", err))
	}
	// 模块 2 未确认接收也视为投递失败，按退避策略重新进入待处理状态。
	accepted, err := gateway.Sync(ctx, event)
	if err != nil || !accepted {
		return true, repository.retryOrderDispatchOutbox(ctx, entry, now, deliveryError(accepted, err))
	}
	return true, repository.markOrderDispatchOutboxDelivered(ctx, entry.ID, now)
}

func (repository *DispatchRepository) claimOrderDispatchOutbox(ctx context.Context, now time.Time, lease time.Duration) (orderDispatchOutboxEntry, bool, error) {
	// SKIP LOCKED 允许多个实例并行消费，过期租约保证进程异常后消息可恢复。
	row := repository.data.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM public.order_dispatch_outbox
		WHERE (status=$1 AND next_attempt_at <= $2) OR (status=$3 AND lease_expires_at <= $2)
		ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
	) UPDATE public.order_dispatch_outbox box SET status=$3,lease_expires_at=$4,updated_at=CURRENT_TIMESTAMP
	FROM candidate WHERE box.id=candidate.id RETURNING box.id,box.payload,box.attempt_count`,
		orderDispatchOutboxPending, now, orderDispatchOutboxSending, now.Add(lease))
	var entry orderDispatchOutboxEntry
	err := row.Scan(&entry.ID, &entry.Payload, &entry.AttemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderDispatchOutboxEntry{}, false, nil
	}
	if err != nil {
		return orderDispatchOutboxEntry{}, false, fmt.Errorf("claim order dispatch outbox: %w", err)
	}
	return entry, true, nil
}

func (repository *DispatchRepository) markOrderDispatchOutboxDelivered(ctx context.Context, id int64, now time.Time) error {
	result, err := repository.data.pool.Exec(ctx, `UPDATE public.order_dispatch_outbox
		SET status=$2,delivered_at=$3,lease_expires_at=NULL,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status=$4`, id, orderDispatchOutboxDelivered, now, orderDispatchOutboxSending)
	if err != nil {
		return fmt.Errorf("mark order dispatch outbox delivered: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mark order dispatch outbox delivered: outbox state conflict")
	}
	return nil
}

func (repository *DispatchRepository) retryOrderDispatchOutbox(ctx context.Context, entry orderDispatchOutboxEntry, now time.Time, cause error) error {
	result, err := repository.data.pool.Exec(ctx, `UPDATE public.order_dispatch_outbox
		SET status=$2,attempt_count=attempt_count+1,next_attempt_at=$3,lease_expires_at=NULL,last_error=$4,updated_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status=$5`, entry.ID, orderDispatchOutboxPending,
		now.Add(dispatchOutboxRetryDelay(entry.AttemptCount)), truncateOutboxError(cause), orderDispatchOutboxSending)
	if err != nil {
		return fmt.Errorf("retry order dispatch outbox: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("retry order dispatch outbox: outbox state conflict")
	}
	return nil
}
