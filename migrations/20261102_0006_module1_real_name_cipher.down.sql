-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 实名信息密文列（DOWN）
--
-- ⚠ DDL 属于高危操作（AGENT.md §3.5），需评审通过后由数据库负责人执行。
-- 注意：回滚会删除密文列，掩码与哈希列保留，实名审核流程不受影响。
-- ============================================================================

BEGIN;

ALTER TABLE passenger_real_name_records
    DROP COLUMN IF EXISTS real_name_cipher,
    DROP COLUMN IF EXISTS id_card_cipher;

COMMIT;
