-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 实名认证记录表（UP）
--
-- 目标库：小组线上库 gva（PostgreSQL）
-- 新增表：passenger_real_name_records
-- 涉及既有表：passengers.real_name_status（提交后置为 1 认证中，无需改表结构）
--
-- ⚠ 执行前置条件（AGENT.md §8.1）：DDL 属于高危操作，必须由组内评审通过后执行；
--   AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261027_0002_module1_passenger_real_name.down.sql
--
-- 数据安全说明（AGENT.md §10）：
--   本表**不保存姓名与身份证号明文**，只保存掩码（前端展示）与加盐 SHA-256 哈希（判重）。
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS passenger_real_name_records (
    id               BIGSERIAL    PRIMARY KEY,
    passenger_id     BIGINT       NOT NULL REFERENCES passengers (id),
    real_name_masked VARCHAR(60)  NOT NULL,
    id_card_masked   VARCHAR(32)  NOT NULL,
    id_card_hash     CHAR(64)     NOT NULL,
    status           SMALLINT     NOT NULL DEFAULT 1,
    reject_reason    VARCHAR(200) NOT NULL DEFAULT '',
    submitted_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    verified_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- 一名乘客只保留一条认证记录，重新提交走覆盖更新
    CONSTRAINT uk_passenger_real_name_passenger UNIQUE (passenger_id),
    CONSTRAINT ck_passenger_real_name_status CHECK (status IN (1, 2, 3))
);

COMMENT ON TABLE passenger_real_name_records IS '乘客实名认证记录（不保存证件明文）';
COMMENT ON COLUMN passenger_real_name_records.real_name_masked IS '姓名掩码，如 张*';
COMMENT ON COLUMN passenger_real_name_records.id_card_masked IS '身份证掩码，如 110105********002X';
COMMENT ON COLUMN passenger_real_name_records.id_card_hash IS '加盐 SHA-256（盐值取自环境变量 RIDE_ID_CARD_SALT），用于证件唯一性判重';
COMMENT ON COLUMN passenger_real_name_records.status IS '认证状态：1认证中 2已认证 3已驳回';
COMMENT ON COLUMN passenger_real_name_records.reject_reason IS '驳回原因，仅 status=3 时有值';
COMMENT ON COLUMN passenger_real_name_records.verified_at IS '审核完成时间（通过/驳回）';

-- 同一证件只能绑定一个账号：仅「认证中/已认证」参与唯一性，驳回后允许重新提交
CREATE UNIQUE INDEX IF NOT EXISTS uk_passenger_real_name_id_card
    ON passenger_real_name_records (id_card_hash)
    WHERE status IN (1, 2);

-- 后台审核队列检索：按状态 + 提交时间倒序
CREATE INDEX IF NOT EXISTS idx_passenger_real_name_status
    ON passenger_real_name_records (status, submitted_at DESC);

COMMIT;
