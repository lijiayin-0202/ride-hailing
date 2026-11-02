-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 密码登录兼容（UP）
--
-- 需求来源：UA-T0-05「密码登录兼容：老接口保留，与新登录共用用户表」
-- 目标库：小组线上库 gva（PostgreSQL）
-- 变更表：passengers 追加 password_hash
--
-- ⚠ 执行前置条件（AGENT.md §3.5）：DDL 属于高危操作，必须由组内评审通过后执行；
--   AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261102_0005_module1_passenger_password.down.sql
--
-- 数据安全说明：
--   1. 只存 bcrypt 哈希，禁止存明文或可逆密文；
--   2. 手机号验证码注册的账号默认无密码（空串），校验层必须拒绝空密码登录。
-- ============================================================================

BEGIN;

ALTER TABLE passengers ADD COLUMN IF NOT EXISTS password_hash VARCHAR(100) NOT NULL DEFAULT '';

COMMENT ON COLUMN passengers.password_hash IS 'bcrypt 密码哈希；空串表示未设置密码（手机号注册账号默认无密码，禁止空密码登录）';

COMMIT;
