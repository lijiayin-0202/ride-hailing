-- ============================================================================
-- 模块1 用户账号与个人中心（成员A）—— 实名信息密文列（UP）
--
-- 需求来源：UA-T0-09「敏感信息保护：AES-256-GCM 加密 + HMAC 检索 + 脱敏返回」
-- 目标库：小组线上库 gva（PostgreSQL）
-- 变更表：passenger_real_name_records 追加 real_name_cipher / id_card_cipher
--
-- ⚠ 执行前置条件（AGENT.md §3.5）：DDL 属于高危操作，必须由组内评审通过后执行；
--   AI 不直接执行本脚本。执行人：数据库负责人 / 组长。
-- 回滚：同目录 20261102_0006_module1_real_name_cipher.down.sql
--
-- 设计说明：
--   1. 原有 real_name_masked / id_card_masked 仍保留：接口只返回掩码；
--   2. id_card_hash（加盐 SHA-256）继续承担唯一性与判重，不用于反解；
--   3. 新增两列只存 AES-256-GCM 密文（格式 v1:<base64(nonce|密文|tag)>），
--      供核验/合规调取场景在服务端解密，任何接口都不返回解密结果；
--   4. 存量数据的密文列为空串（表示未加密回填），需要时由运维脚本按密钥回填。
-- ============================================================================

BEGIN;

ALTER TABLE passenger_real_name_records
    ADD COLUMN IF NOT EXISTS real_name_cipher TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS id_card_cipher   TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN passenger_real_name_records.real_name_cipher IS '真实姓名 AES-256-GCM 密文，格式 v1:<base64(nonce|cipher|tag)>';
COMMENT ON COLUMN passenger_real_name_records.id_card_cipher IS '身份证号 AES-256-GCM 密文，格式 v1:<base64(nonce|cipher|tag)>';

COMMIT;
