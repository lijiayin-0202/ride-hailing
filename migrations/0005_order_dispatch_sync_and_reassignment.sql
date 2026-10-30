-- 两类 Outbox 都与派单记录同事务提交：保证业务事实落库后才允许异步跨服务调用。
BEGIN;

CREATE TABLE public.order_dispatch_outbox (
    id BIGINT PRIMARY KEY,
    dispatch_id BIGINT NOT NULL,
    idempotency_key VARCHAR(50) NOT NULL,
    event_type SMALLINT NOT NULL,
    payload JSONB NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_expires_at TIMESTAMPTZ(6),
    last_error VARCHAR(500),
    delivered_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_order_dispatch_outbox_dispatch
        FOREIGN KEY (dispatch_id) REFERENCES public.dispatch_records (id),
    -- 同一派单阶段只能生成一条消息，使重新执行终态处理保持幂等。
    CONSTRAINT order_dispatch_outbox_dispatch_event_key
        UNIQUE (dispatch_id, event_type),
    CONSTRAINT order_dispatch_outbox_status_check CHECK (status IN (0, 1, 2)),
    CONSTRAINT order_dispatch_outbox_attempt_count_check CHECK (attempt_count >= 0),
    CONSTRAINT order_dispatch_outbox_delivery_state_check CHECK (
        (status = 2 AND delivered_at IS NOT NULL AND lease_expires_at IS NULL)
        OR (status IN (0, 1) AND delivered_at IS NULL)
    )
);

CREATE INDEX idx_order_dispatch_outbox_ready
    ON public.order_dispatch_outbox (status, next_attempt_at)
    WHERE status = 0;

CREATE INDEX idx_order_dispatch_outbox_lease
    ON public.order_dispatch_outbox (status, lease_expires_at)
    WHERE status = 1;

CREATE TABLE public.dispatch_reassignment_jobs (
    id BIGINT PRIMARY KEY,
    order_id BIGINT NOT NULL,
    trigger_dispatch_id BIGINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_expires_at TIMESTAMPTZ(6),
    last_error VARCHAR(500),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_dispatch_reassignment_job_dispatch
        FOREIGN KEY (trigger_dispatch_id) REFERENCES public.dispatch_records (id),
    -- 每个拒绝或超时的触发派单最多创建一次补派任务。
    CONSTRAINT dispatch_reassignment_jobs_trigger_dispatch_key
        UNIQUE (trigger_dispatch_id),
    CONSTRAINT dispatch_reassignment_jobs_status_check CHECK (status IN (0, 1, 2)),
    CONSTRAINT dispatch_reassignment_jobs_attempt_count_check CHECK (attempt_count >= 0)
);

CREATE INDEX idx_dispatch_reassignment_jobs_ready
    ON public.dispatch_reassignment_jobs (status, next_attempt_at)
    WHERE status = 0;

CREATE INDEX idx_dispatch_reassignment_jobs_lease
    ON public.dispatch_reassignment_jobs (status, lease_expires_at)
    WHERE status = 1;

COMMIT;
