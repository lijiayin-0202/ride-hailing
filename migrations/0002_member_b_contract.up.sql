-- 0002 成员 B 契约收口（对齐线上 robotaxi schema v1.0.0-40/50 的实际结构）
--
-- 线上库已具备：trip_orders.status 0–7、uk_passenger_active_trip、uk_order_successful_payment、
-- trip_reviews_order_id_key、idx_trip_location_order_time、order_cancellations、refunds 等，
-- 因此本脚本只做两处增量，不再重映射状态、不建重复索引、不改任何字段。
BEGIN;
SET search_path TO robotaxi, public;

-- 1. order_commands 命令枚举扩展
--    现状仅允许 CANCEL / UNLOCK_AND_START / REQUEST_PULL_OVER，调度与车端推进状态所需命令会直接违反约束。
ALTER TABLE order_commands DROP CONSTRAINT IF EXISTS order_commands_command_type_check;
ALTER TABLE order_commands ADD CONSTRAINT order_commands_command_type_check CHECK (
    command_type::text = ANY (ARRAY[
        'CANCEL', 'UNLOCK_AND_START', 'REQUEST_PULL_OVER',
        'DISPATCH', 'DISPATCH_ACCEPT', 'MARK_ARRIVED',
        'START_TRIP', 'COMPLETE_TRIP', 'CLOSE_EXCEPTION'
    ]::text[])
);

-- 2. 费用明细金额符号口径：仅优惠券 fee_type=5 允许负数，其余费用项必须非负。
--    线上当前无负数金额记录，可直接加约束。
ALTER TABLE order_fee_items DROP CONSTRAINT IF EXISTS ck_order_fee_items_amount_sign;
ALTER TABLE order_fee_items ADD CONSTRAINT ck_order_fee_items_amount_sign
    CHECK (amount >= 0 OR fee_type = 5);

-- 3. 迁移台账
INSERT INTO schema_versions (version_no, description)
VALUES ('2.0.0-40-member-b', '成员B：订单命令枚举扩展与费用明细金额符号约束')
ON CONFLICT (version_no) DO NOTHING;

COMMIT;
