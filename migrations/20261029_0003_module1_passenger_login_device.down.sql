-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 登录设备与登录记录（DOWN / 回滚）
--
-- ⚠ 高危操作：会删除登录设备表并移除登录记录的设备/归属地字段，仅供评审通过后执行。
-- ============================================================================

BEGIN;

DROP INDEX IF EXISTS idx_passenger_auth_records_time;

ALTER TABLE passenger_auth_records DROP COLUMN IF EXISTS login_location;
ALTER TABLE passenger_auth_records DROP COLUMN IF EXISTS device_model;
ALTER TABLE passenger_auth_records DROP COLUMN IF EXISTS device_name;

DROP TABLE IF EXISTS passenger_login_devices;

COMMIT;
