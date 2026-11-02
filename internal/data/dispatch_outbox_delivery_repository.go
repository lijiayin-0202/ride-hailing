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
	dispatchOutboxPending int16 = iota
	dispatchOutboxSending
	dispatchOutboxDelivered
)

type dispatchOutboxEntry struct {
	ID           int64
	Payload      []byte
	AttemptCount int
}

// ProcessOutboxOnce 领取一条到期记录并执行一次模块 4 派单投递。
func (repository *DispatchRepository) ProcessOutboxOnce(ctx context.Context, gateway biz.VehicleDispatchGateway, now time.Time, lease time.Duration) (bool, error) {
	entry, found, err := repository.claimDispatchOutbox(ctx, now, lease)
	if err != nil || !found {
		return found, err
	}
	var instruction biz.VehicleDispatchInstruction
	if err := json.Unmarshal(entry.Payload, &instruction); err != nil {
		return true, repository.retryDispatchOutbox(ctx, entry, now, fmt.Errorf("decode outbox payload: %w", err))
	}
	// 外部调用在事务外执行；失败只调整 Outbox 重试状态，不回滚已经确认的派单事实。
	accepted, err := gateway.Send(ctx, instruction)
	if err != nil || !accepted {
		return true, repository.retryDispatchOutbox(ctx, entry, now, deliveryError(accepted, err))
	}
	return true, repository.markDispatchOutboxDelivered(ctx, entry.ID, now)
}

func (repository *DispatchRepository) claimDispatchOutbox(ctx context.Context, now time.Time, lease time.Duration) (dispatchOutboxEntry, bool, error) {
	// SKIP LOCKED 允许多个实例并发拉取，过期租约可回收宕机实例遗留的处理中消息。
	row := repository.data.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM public.dispatch_outbox
			WHERE (status = $1 AND next_attempt_at <= $2)
				OR (status = $3 AND lease_expires_at <= $2)
			ORDER BY next_attempt_at ASC, id ASC
			LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		UPDATE public.dispatch_outbox AS outbox
		SET status = $3, lease_expires_at = $4, updated_at = CURRENT_TIMESTAMP
		FROM candidate WHERE outbox.id = candidate.id
		RETURNING outbox.id, outbox.payload, outbox.attempt_count`,
		dispatchOutboxPending, now, dispatchOutboxSending, now.Add(lease))
	var entry dispatchOutboxEntry
	err := row.Scan(&entry.ID, &entry.Payload, &entry.AttemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return dispatchOutboxEntry{}, false, nil
	}
	if err != nil {
		return dispatchOutboxEntry{}, false, fmt.Errorf("claim dispatch outbox: %w", err)
	}
	return entry, true, nil
}

func (repository *DispatchRepository) markDispatchOutboxDelivered(ctx context.Context, outboxID int64, now time.Time) error {
	result, err := repository.data.pool.Exec(ctx, `
		UPDATE public.dispatch_outbox
		SET status = $2, delivered_at = $3, lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = $4`, outboxID, dispatchOutboxDelivered, now, dispatchOutboxSending)
	if err != nil {
		return fmt.Errorf("mark dispatch outbox delivered: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mark dispatch outbox delivered: outbox state conflict")
	}
	return nil
}

func (repository *DispatchRepository) retryDispatchOutbox(ctx context.Context, entry dispatchOutboxEntry, now time.Time, cause error) error {
	result, err := repository.data.pool.Exec(ctx, `
		UPDATE public.dispatch_outbox
		SET status = $2, attempt_count = attempt_count + 1, next_attempt_at = $3,
			lease_expires_at = NULL, last_error = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = $5`,
		entry.ID, dispatchOutboxPending, now.Add(dispatchOutboxRetryDelay(entry.AttemptCount)), truncateOutboxError(cause), dispatchOutboxSending)
	if err != nil {
		return fmt.Errorf("retry dispatch outbox: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("retry dispatch outbox: outbox state conflict")
	}
	return nil
}

func dispatchOutboxRetryDelay(attemptCount int) time.Duration {
	// 指数退避降低模块 4 故障期间的请求压力，并在五次后封顶为 30 秒。
	if attemptCount >= 5 {
		return 30 * time.Second
	}
	return time.Second << attemptCount
}

func deliveryError(accepted bool, err error) error {
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("module 4 did not accept dispatch delivery")
	}
	return nil
}

func truncateOutboxError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) <= 500 {
		return message
	}
	return message[:500]
}
