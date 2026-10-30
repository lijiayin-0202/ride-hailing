-- 模块 4 派单指令采用 Transactional Outbox，业务提交后才能异步发送。
BEGIN;

CREATE TABLE public.dispatch_outbox (
    id BIGINT PRIMARY KEY,
    dispatch_id BIGINT NOT NULL,
    idempotency_key VARCHAR(50) NOT NULL,
    payload JSONB NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_expires_at TIMESTAMPTZ(6),
    last_error VARCHAR(500),
    delivered_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_dispatch_outbox_dispatch
        FOREIGN KEY (dispatch_id) REFERENCES public.dispatch_records (id),
    -- 每条派单记录只能生成一份车辆指令，避免网络重试导致重复接驾任务。
    CONSTRAINT dispatch_outbox_dispatch_id_key UNIQUE (dispatch_id),
    CONSTRAINT dispatch_outbox_idempotency_key_key UNIQUE (idempotency_key),
    CONSTRAINT dispatch_outbox_status_check CHECK (status IN (0, 1, 2)),
    CONSTRAINT dispatch_outbox_attempt_count_check CHECK (attempt_count >= 0),
    CONSTRAINT dispatch_outbox_delivery_state_check CHECK (
        (status = 2 AND delivered_at IS NOT NULL AND lease_expires_at IS NULL)
        OR (status IN (0, 1) AND delivered_at IS NULL)
    )
);

-- 待发送与租约恢复走不同部分索引，支持多实例并发领取。
CREATE INDEX idx_dispatch_outbox_ready
    ON public.dispatch_outbox (status, next_attempt_at)
    WHERE status = 0;

CREATE INDEX idx_dispatch_outbox_lease
    ON public.dispatch_outbox (status, lease_expires_at)
    WHERE status = 1;

COMMIT;
