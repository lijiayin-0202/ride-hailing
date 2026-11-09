-- 0002 回滚：恢复 order_commands 的三值命令枚举，移除费用金额符号约束。
-- 回滚前须确认不存在 DISPATCH/车端命令产生的 open 订单（status 1/2/3/4）。
BEGIN;
SET search_path TO robotaxi, public;

ALTER TABLE order_commands DROP CONSTRAINT IF EXISTS order_commands_command_type_check;
ALTER TABLE order_commands ADD CONSTRAINT order_commands_command_type_check CHECK (
    command_type::text = ANY (ARRAY['CANCEL', 'UNLOCK_AND_START', 'REQUEST_PULL_OVER']::text[])
);

ALTER TABLE order_fee_items DROP CONSTRAINT IF EXISTS ck_order_fee_items_amount_sign;

DELETE FROM schema_versions WHERE version_no = '2.0.0-40-member-b';

COMMIT;
