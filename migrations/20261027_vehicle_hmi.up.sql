-- 模块四复用数据字典中已有的 robotaxi 业务表，仅补齐幂等索引。
-- 本脚本不创建重复业务表，也不修改共享字段或约束。

CREATE INDEX IF NOT EXISTS idx_vehicle_commands_pending
    ON robotaxi.vehicle_commands (vehicle_id, status, priority, issued_at);

CREATE INDEX IF NOT EXISTS idx_boarding_order_time
    ON robotaxi.boarding_verifications (order_id, verified_at DESC);

CREATE INDEX IF NOT EXISTS idx_vehicle_faults_open
    ON robotaxi.vehicle_faults (vehicle_id, status, occurred_at DESC)
    WHERE status IN (0, 1);

CREATE INDEX IF NOT EXISTS idx_vehicle_location_vehicle_time
    ON robotaxi.vehicle_location_logs (vehicle_id, reported_at DESC);

CREATE INDEX IF NOT EXISTS idx_vehicle_location_order_time
    ON robotaxi.vehicle_location_logs (order_id, reported_at DESC);

CREATE INDEX IF NOT EXISTS idx_vehicle_status_vehicle_time
    ON robotaxi.vehicle_status_logs (vehicle_id, changed_at DESC);

CREATE INDEX IF NOT EXISTS idx_vehicle_telemetry_vehicle_time
    ON robotaxi.vehicle_telemetry_logs (vehicle_id, reported_at DESC);

CREATE INDEX IF NOT EXISTS idx_vehicle_media_order_time
    ON robotaxi.vehicle_media_records (related_order_id, started_at DESC);
