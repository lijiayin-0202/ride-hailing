-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— PostgreSQL 迁移脚本（UP）
--
-- 目标库：小组线上库 gva（PostgreSQL）
-- 涉及表：passengers、passenger_auth_records、passenger_addresses、
--         coupons、passenger_coupons、passenger_wallets、wallet_transactions、
--         point_transactions、sign_in_records、notifications
--
-- ⚠ 执行前置条件（AGENT.md §8.1、§11）：DDL 属于高危操作，必须由组内评审通过后再执行，
--    AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261023_0001_module1_passenger_account.down.sql（按依赖倒序删除）
--
-- 跨模块外键说明：station_id（→ stations.id）、service_area_id（→ service_areas.id）、
-- related_order_id（→ trip_orders.id）所属表由模块2/模块3交付，本脚本只建列与索引、
-- 不建外键，待对端表落地后再由各自迁移补齐，避免迁移顺序耦合。
-- ============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. 乘客账户
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS passengers (
    id               BIGSERIAL    PRIMARY KEY,
    passenger_no     VARCHAR(32)  NOT NULL,
    mobile           VARCHAR(20)  NOT NULL,
    nickname         VARCHAR(60)  NOT NULL DEFAULT '',
    avatar_url       VARCHAR(500) NOT NULL DEFAULT '',
    real_name_status SMALLINT     NOT NULL DEFAULT 0,
    account_status   SMALLINT     NOT NULL DEFAULT 1,
    point_balance    INTEGER      NOT NULL DEFAULT 0,
    preferences      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    last_login_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_passengers_no UNIQUE (passenger_no),
    CONSTRAINT uk_passengers_mobile UNIQUE (mobile),
    CONSTRAINT ck_passengers_real_name_status CHECK (real_name_status IN (0, 1, 2, 3)),
    CONSTRAINT ck_passengers_account_status CHECK (account_status IN (0, 1, 2)),
    CONSTRAINT ck_passengers_point_balance CHECK (point_balance >= 0)
);

COMMENT ON TABLE passengers IS '乘客账户（登录主体）';
COMMENT ON COLUMN passengers.passenger_no IS '乘客编号，业务唯一标识';
COMMENT ON COLUMN passengers.mobile IS '手机号（等值登录查询）；日志/接口输出必须脱敏';
COMMENT ON COLUMN passengers.real_name_status IS '实名状态：0未认证 1认证中 2已认证 3认证驳回';
COMMENT ON COLUMN passengers.account_status IS '账号状态：0冻结 1正常 2注销';
COMMENT ON COLUMN passengers.point_balance IS '积分余额（以 point_transactions 流水为依据的对账冗余）';
COMMENT ON COLUMN passengers.preferences IS '通知/隐私/显示偏好 JSONB';

-- 手机号唯一索引由 uk_passengers_mobile 覆盖，满足登录等值查询；
-- 冻结/注销账号的运营检索按状态+更新时间过滤。
CREATE INDEX IF NOT EXISTS idx_passengers_status_updated
    ON passengers (account_status, updated_at DESC);

-- ---------------------------------------------------------------------------
-- 2. 登录审计
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS passenger_auth_records (
    id             BIGSERIAL    PRIMARY KEY,
    passenger_id   BIGINT       REFERENCES passengers (id),
    mobile_masked  VARCHAR(20)  NOT NULL,
    auth_type      SMALLINT     NOT NULL DEFAULT 1,
    login_result   SMALLINT     NOT NULL,
    failure_reason VARCHAR(200) NOT NULL DEFAULT '',
    device_id      VARCHAR(64)  NOT NULL DEFAULT '',
    client_ip      VARCHAR(64)  NOT NULL DEFAULT '',
    user_agent     VARCHAR(255) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ck_passenger_auth_records_result CHECK (login_result IN (0, 1))
);

COMMENT ON TABLE passenger_auth_records IS '乘客登录审计（不保存验证码/令牌明文）';
COMMENT ON COLUMN passenger_auth_records.mobile_masked IS '脱敏手机号，如 138****0000';
COMMENT ON COLUMN passenger_auth_records.auth_type IS '认证方式：1手机号验证码';

CREATE INDEX IF NOT EXISTS idx_passenger_auth_records_passenger
    ON passenger_auth_records (passenger_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 3. 常用地址
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS passenger_addresses (
    id           BIGSERIAL     PRIMARY KEY,
    passenger_id BIGINT        NOT NULL REFERENCES passengers (id),
    address_type SMALLINT      NOT NULL,
    address_name VARCHAR(60)   NOT NULL,
    address      VARCHAR(255)  NOT NULL,
    longitude    NUMERIC(10,6) NOT NULL DEFAULT 0,
    latitude     NUMERIC(10,6) NOT NULL DEFAULT 0,
    station_id   BIGINT,
    is_default   BOOLEAN       NOT NULL DEFAULT false,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT ck_passenger_addresses_type CHECK (address_type IN (1, 2, 3)),
    CONSTRAINT ck_passenger_addresses_longitude CHECK (longitude BETWEEN -180 AND 180),
    CONSTRAINT ck_passenger_addresses_latitude CHECK (latitude BETWEEN -90 AND 90)
);

COMMENT ON TABLE passenger_addresses IS '乘客常用地址（家/公司/常用站点）';
COMMENT ON COLUMN passenger_addresses.address_type IS '地址类型：1家 2公司 3常用站点';
COMMENT ON COLUMN passenger_addresses.station_id IS '关联 stations.id（模块3交付后补外键）';
COMMENT ON COLUMN passenger_addresses.is_default IS '是否默认地址；同类型默认唯一';

CREATE INDEX IF NOT EXISTS idx_passenger_addresses_passenger
    ON passenger_addresses (passenger_id, address_type);

-- 默认地址唯一约束：同一乘客同一类型只允许一个默认地址（部分唯一索引）
CREATE UNIQUE INDEX IF NOT EXISTS uk_passenger_addresses_default
    ON passenger_addresses (passenger_id, address_type)
    WHERE is_default;

-- ---------------------------------------------------------------------------
-- 4. 优惠券模板与持券实例
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS coupons (
    id                   BIGSERIAL     PRIMARY KEY,
    coupon_code          VARCHAR(40)   NOT NULL,
    coupon_name          VARCHAR(100)  NOT NULL,
    coupon_type          SMALLINT      NOT NULL DEFAULT 1,
    discount_amount      NUMERIC(10,2) NOT NULL DEFAULT 0,
    discount_rate        NUMERIC(5,2)  NOT NULL DEFAULT 0,
    minimum_order_amount NUMERIC(10,2) NOT NULL DEFAULT 0,
    service_area_id      BIGINT,
    valid_from           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    valid_to             TIMESTAMPTZ   NOT NULL DEFAULT now(),
    status               SMALLINT      NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uk_coupons_code UNIQUE (coupon_code),
    CONSTRAINT ck_coupons_type CHECK (coupon_type IN (1, 2, 3)),
    CONSTRAINT ck_coupons_status CHECK (status IN (0, 1))
);

COMMENT ON TABLE coupons IS '优惠券模板';
COMMENT ON COLUMN coupons.coupon_type IS '券类型：1立减 2满减 3折扣';
COMMENT ON COLUMN coupons.service_area_id IS '适用运营区域，空表示不限（关联 service_areas.id）';

CREATE TABLE IF NOT EXISTS passenger_coupons (
    id                 BIGSERIAL    PRIMARY KEY,
    passenger_id       BIGINT       NOT NULL REFERENCES passengers (id),
    coupon_id          BIGINT       NOT NULL REFERENCES coupons (id),
    coupon_instance_no VARCHAR(40)  NOT NULL,
    status             SMALLINT     NOT NULL DEFAULT 0,
    related_order_id   BIGINT,
    received_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    used_at            TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ  NOT NULL,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_passenger_coupons_instance UNIQUE (coupon_instance_no),
    CONSTRAINT ck_passenger_coupons_status CHECK (status IN (0, 1, 2, 3))
);

COMMENT ON TABLE passenger_coupons IS '乘客持券实例（券包）';
COMMENT ON COLUMN passenger_coupons.status IS '状态：0未使用 1已锁定 2已使用 3已过期';
COMMENT ON COLUMN passenger_coupons.related_order_id IS '核销/锁定关联订单（trip_orders.id，模块2）';

CREATE INDEX IF NOT EXISTS idx_passenger_coupons_passenger
    ON passenger_coupons (passenger_id, status, expires_at);

-- ---------------------------------------------------------------------------
-- 5. 钱包与资金流水（模块1建表，模块2支付复用；金额正负规则见 wallet_transactions）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS passenger_wallets (
    id            BIGSERIAL     PRIMARY KEY,
    passenger_id  BIGINT        NOT NULL REFERENCES passengers (id),
    balance       NUMERIC(12,2) NOT NULL DEFAULT 0,
    frozen_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    version       INTEGER       NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uk_passenger_wallets_passenger UNIQUE (passenger_id),
    CONSTRAINT ck_passenger_wallets_balance CHECK (balance >= 0),
    CONSTRAINT ck_passenger_wallets_frozen CHECK (frozen_amount >= 0)
);

COMMENT ON TABLE passenger_wallets IS '乘客钱包（余额唯一权威来源，version 用于乐观锁）';

CREATE TABLE IF NOT EXISTS wallet_transactions (
    id               BIGSERIAL     PRIMARY KEY,
    transaction_no   VARCHAR(40)   NOT NULL,
    passenger_id     BIGINT        NOT NULL REFERENCES passengers (id),
    wallet_id        BIGINT        NOT NULL REFERENCES passenger_wallets (id),
    transaction_type SMALLINT      NOT NULL,
    amount           NUMERIC(12,2) NOT NULL,
    balance_after    NUMERIC(12,2) NOT NULL,
    related_order_id BIGINT,
    remark           VARCHAR(255)  NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT uk_wallet_transactions_no UNIQUE (transaction_no),
    CONSTRAINT ck_wallet_transactions_type CHECK (transaction_type IN (1, 2, 3, 4, 5))
);

COMMENT ON TABLE wallet_transactions IS '钱包资金流水；金额正负表示收支方向，禁止用订单金额替代';
COMMENT ON COLUMN wallet_transactions.transaction_type IS '类型：1充值 2支付 3退款 4冻结 5解冻';

CREATE INDEX IF NOT EXISTS idx_wallet_transactions_passenger
    ON wallet_transactions (passenger_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 6. 积分流水
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS point_transactions (
    id               BIGSERIAL    PRIMARY KEY,
    transaction_no   VARCHAR(40)  NOT NULL,
    passenger_id     BIGINT       NOT NULL REFERENCES passengers (id),
    change_amount    INTEGER      NOT NULL,
    balance_after    INTEGER      NOT NULL,
    source_type      SMALLINT     NOT NULL,
    related_order_id BIGINT,
    remark           VARCHAR(255) NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_point_transactions_no UNIQUE (transaction_no),
    CONSTRAINT ck_point_transactions_source CHECK (source_type IN (1, 2, 3, 4)),
    CONSTRAINT ck_point_transactions_balance CHECK (balance_after >= 0)
);

COMMENT ON TABLE point_transactions IS '积分流水；change_amount 正为获得、负为消耗';
COMMENT ON COLUMN point_transactions.source_type IS '来源：1完成行程 2每日签到 3兑换消耗 4人工调整';

CREATE INDEX IF NOT EXISTS idx_point_transactions_passenger
    ON point_transactions (passenger_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 7. 每日签到（(passenger_id, sign_in_date) 唯一，保证一天一次）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sign_in_records (
    id              BIGSERIAL   PRIMARY KEY,
    passenger_id    BIGINT      NOT NULL REFERENCES passengers (id),
    sign_in_date    DATE        NOT NULL,
    reward_points   INTEGER     NOT NULL DEFAULT 0,
    continuous_days INTEGER     NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_sign_in_records_day UNIQUE (passenger_id, sign_in_date),
    CONSTRAINT ck_sign_in_records_reward CHECK (reward_points >= 0),
    CONSTRAINT ck_sign_in_records_continuous CHECK (continuous_days >= 1)
);

COMMENT ON TABLE sign_in_records IS '乘客每日签到记录（业务时区按北京时间自然日）';

-- ---------------------------------------------------------------------------
-- 8. 消息中心
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notifications (
    id                BIGSERIAL     PRIMARY KEY,
    passenger_id      BIGINT        NOT NULL REFERENCES passengers (id),
    notification_type SMALLINT      NOT NULL DEFAULT 5,
    title             VARCHAR(120)  NOT NULL,
    content           VARCHAR(1000) NOT NULL DEFAULT '',
    related_order_id  BIGINT,
    is_read           BOOLEAN       NOT NULL DEFAULT false,
    read_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT ck_notifications_type CHECK (notification_type IN (1, 2, 3, 4, 5))
);

COMMENT ON TABLE notifications IS '乘客消息（消息生产方为模块5，本模块维护已读状态）';
COMMENT ON COLUMN notifications.notification_type IS '类型：1订单 2支付 3活动 4安全 5系统';

CREATE INDEX IF NOT EXISTS idx_notifications_passenger_unread
    ON notifications (passenger_id, is_read, created_at DESC);

COMMIT;
