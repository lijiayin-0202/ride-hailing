-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 登录设备与登录记录（UP）
--
-- 目标库：小组线上库 gva（PostgreSQL）
-- 新增表：passenger_login_devices（登录设备）
-- 变更表：passenger_auth_records 追加 device_name / device_model / login_location
--
-- ⚠ 执行前置条件（AGENT.md §8.1）：DDL 属于高危操作，必须由组内评审通过后执行；
--   AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261029_0003_module1_passenger_login_device.down.sql
--
-- 数据安全说明（AGENT.md §10）：
--   1. 设备表不保存 IMEI/MAC 等硬件唯一标识，只保存客户端自生成的 device_id；
--   2. IP 仅用于安全审计与归属地展示，接口返回时统一脱敏（见 pkg/iputil）。
-- ============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. 登录设备（每个乘客每个设备一条，重复登录只更新计数与时间）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS passenger_login_devices (
    id                  BIGSERIAL    PRIMARY KEY,
    passenger_id        BIGINT       NOT NULL REFERENCES passengers (id),
    device_id           VARCHAR(64)  NOT NULL,
    device_name         VARCHAR(80)  NOT NULL DEFAULT '',
    device_model        VARCHAR(60)  NOT NULL DEFAULT '',
    os_name             VARCHAR(30)  NOT NULL DEFAULT '',
    os_version          VARCHAR(30)  NOT NULL DEFAULT '',
    app_version         VARCHAR(30)  NOT NULL DEFAULT '',
    last_login_ip       VARCHAR(64)  NOT NULL DEFAULT '',
    last_login_location VARCHAR(120) NOT NULL DEFAULT '',
    login_count         INTEGER      NOT NULL DEFAULT 1,
    first_login_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_passenger_login_devices UNIQUE (passenger_id, device_id),
    CONSTRAINT ck_passenger_login_devices_count CHECK (login_count >= 1)
);

COMMENT ON TABLE passenger_login_devices IS '乘客登录设备（账号管理页「登录设备」）';
COMMENT ON COLUMN passenger_login_devices.device_id IS '客户端生成并持久化的设备编号，非硬件唯一标识';
COMMENT ON COLUMN passenger_login_devices.device_name IS '设备名称，如 iPhone · Safari';
COMMENT ON COLUMN passenger_login_devices.device_model IS '设备型号，如 iPhone 15 / SM-G9910 / PC';
COMMENT ON COLUMN passenger_login_devices.os_name IS '操作系统，如 iOS / Android / Windows';
COMMENT ON COLUMN passenger_login_devices.app_version IS '客户端版本号（请求头 X-Client-Version）';
COMMENT ON COLUMN passenger_login_devices.last_login_ip IS '最近登录 IP（仅审计用，接口返回脱敏）';
COMMENT ON COLUMN passenger_login_devices.last_login_location IS '最近登录归属地文字（无 IP 库时返回内网/未知归属地）';

-- 列表按最近登录时间倒序，覆盖索引避免回表扫描全表
CREATE INDEX IF NOT EXISTS idx_passenger_login_devices_recent
    ON passenger_login_devices (passenger_id, last_login_at DESC);

-- ---------------------------------------------------------------------------
-- 2. 登录记录补充设备与归属地字段（已有表，仅加列，不改存量字段语义）
-- ---------------------------------------------------------------------------
ALTER TABLE passenger_auth_records ADD COLUMN IF NOT EXISTS device_name     VARCHAR(80)  NOT NULL DEFAULT '';
ALTER TABLE passenger_auth_records ADD COLUMN IF NOT EXISTS device_model    VARCHAR(60)  NOT NULL DEFAULT '';
ALTER TABLE passenger_auth_records ADD COLUMN IF NOT EXISTS login_location  VARCHAR(120) NOT NULL DEFAULT '';

COMMENT ON COLUMN passenger_auth_records.device_name IS '设备名称（登录时由 User-Agent 解析）';
COMMENT ON COLUMN passenger_auth_records.device_model IS '设备型号（登录时由 User-Agent 解析）';
COMMENT ON COLUMN passenger_auth_records.login_location IS '登录归属地文字（无 IP 库时返回内网/未知归属地）';

-- 登录记录列表：按乘客 + 时间倒序分页
CREATE INDEX IF NOT EXISTS idx_passenger_auth_records_time
    ON passenger_auth_records (passenger_id, created_at DESC);

COMMIT;
