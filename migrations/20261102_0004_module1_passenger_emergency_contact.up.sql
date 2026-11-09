-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 紧急联系人（UP）
--
-- 目标库：小组线上库 gva（PostgreSQL）
-- 新增表：passenger_emergency_contacts
--
-- ⚠ 执行前置条件（AGENT.md §3.5）：DDL 属于高危操作，必须由组内评审通过后执行；
--   AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261102_0004_module1_passenger_emergency_contact.down.sql
--
-- 需求来源：T0「账号-支持乘客实名认证与档案维护」——可维护常用地址与紧急联系人。
-- 数据安全说明：
--   1. 联系人手机号属敏感数据，仅用于紧急联系场景，接口返回统一脱敏；
--   2. 最小化采集：只存姓名、手机号与关系，不存证件、住址等额外信息。
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS passenger_emergency_contacts (
    id             BIGSERIAL    PRIMARY KEY,
    passenger_id   BIGINT       NOT NULL REFERENCES passengers (id),
    contact_name   VARCHAR(30)  NOT NULL,
    contact_mobile VARCHAR(20)  NOT NULL,
    relation       VARCHAR(20)  NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- 一名乘客只保留一条紧急联系人，重复保存走覆盖更新
    CONSTRAINT uk_passenger_emergency_contact UNIQUE (passenger_id)
);

COMMENT ON TABLE passenger_emergency_contacts IS '乘客紧急联系人（档案维护）';
COMMENT ON COLUMN passenger_emergency_contacts.contact_name IS '联系人姓名（2-30 字符）';
COMMENT ON COLUMN passenger_emergency_contacts.contact_mobile IS '联系人手机号（接口返回必须脱敏）';
COMMENT ON COLUMN passenger_emergency_contacts.relation IS '与乘客关系，如 配偶/父母/朋友，可为空';

COMMIT;
