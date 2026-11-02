-- 订单表属于模块 2；迁移先显式验证依赖存在，禁止在共享库中悄悄创建替代表。
BEGIN;

DO $$
BEGIN
    IF to_regclass('public.trip_orders') IS NULL THEN
        RAISE EXCEPTION '无法创建 fk_dispatch_records_order：缺少 public.trip_orders';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'dispatch_records'
          AND column_name = 'order_id'
    ) THEN
        RAISE EXCEPTION '无法创建 fk_dispatch_records_order：缺少 public.dispatch_records.order_id';
    END IF;
END
$$;

ALTER TABLE public.dispatch_records
    -- 不允许删改仍被派单记录引用的订单，确保调度历史始终可追溯。
    ADD CONSTRAINT fk_dispatch_records_order
    FOREIGN KEY (order_id)
    REFERENCES public.trip_orders (id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT;

COMMIT;
