-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— PostgreSQL 迁移脚本（DOWN / 回滚）
--
-- ⚠ 高危操作：会删除模块1全部业务表及数据，仅供评审通过后由数据库负责人执行。
--    执行前请确认无其他模块（模块2 钱包/券、模块5 消息）正在引用这些表。
-- ============================================================================

BEGIN;

DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS sign_in_records;
DROP TABLE IF EXISTS point_transactions;
DROP TABLE IF EXISTS wallet_transactions;
DROP TABLE IF EXISTS passenger_wallets;
DROP TABLE IF EXISTS passenger_coupons;
DROP TABLE IF EXISTS coupons;
DROP TABLE IF EXISTS passenger_addresses;
DROP TABLE IF EXISTS passenger_auth_records;
DROP TABLE IF EXISTS passengers;

COMMIT;
