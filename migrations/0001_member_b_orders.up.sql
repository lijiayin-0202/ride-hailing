BEGIN;
SET search_path TO robotaxi, public;

CREATE TABLE IF NOT EXISTS vehicle_service_types (
    id BIGSERIAL PRIMARY KEY,
    service_type_code VARCHAR(64) NOT NULL UNIQUE,
    city_code VARCHAR(32) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    seat_count SMALLINT NOT NULL CHECK (seat_count > 0),
    pickup_eta_seconds INTEGER NOT NULL DEFAULT 300 CHECK (pickup_eta_seconds >= 0),
    status SMALLINT NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_vehicle_service_types_city_status
    ON vehicle_service_types (city_code, status);

ALTER TABLE pricing_rules
    ADD COLUMN IF NOT EXISTS service_type_id BIGINT REFERENCES vehicle_service_types(id),
    ADD COLUMN IF NOT EXISTS version VARCHAR(64),
    ADD COLUMN IF NOT EXISTS base_distance_m INTEGER;

UPDATE pricing_rules
SET base_distance_m = ROUND(included_distance_km * 1000)::INTEGER
WHERE base_distance_m IS NULL;

ALTER TABLE pricing_rules
    ALTER COLUMN base_distance_m SET DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_pricing_rules_active
    ON pricing_rules (service_type_id, status, effective_from, effective_to);

INSERT INTO vehicle_service_types
    (service_type_code, city_code, display_name, tags, seat_count, pickup_eta_seconds)
VALUES
    ('rt6_saver', '420100', 'Apollo RT6 · 特惠', '["全无人"]'::jsonb, 4, 180),
    ('rt6_comfort', '420100', 'Apollo RT6 · 舒享', '["座椅加热"]'::jsonb, 4, 360)
ON CONFLICT (service_type_code) DO NOTHING;

INSERT INTO pricing_rules
    (service_area_id, service_type_id, version, rule_name, vehicle_type,
     base_fare, included_distance_km, distance_unit_price, minimum_fare,
     base_distance_m, effective_from, status)
SELECT area.id, vst.id, '2026-10-v1', '萝卜快跑特惠计价', 'rt6_saver',
       12.60, 3.00, 1.28, 0, 3000, CURRENT_TIMESTAMP, 1
FROM vehicle_service_types vst
JOIN service_areas area ON area.city_code = vst.city_code AND area.status = 1
WHERE vst.service_type_code = 'rt6_saver'
  AND NOT EXISTS (
      SELECT 1 FROM pricing_rules existing
      WHERE existing.service_type_id = vst.id AND existing.version = '2026-10-v1'
  );

INSERT INTO pricing_rules
    (service_area_id, service_type_id, version, rule_name, vehicle_type,
     base_fare, included_distance_km, distance_unit_price, minimum_fare,
     base_distance_m, effective_from, status)
SELECT area.id, vst.id, '2026-10-v1', '萝卜快跑舒享计价', 'rt6_comfort',
       16.00, 3.00, 1.40, 0, 3000, CURRENT_TIMESTAMP, 1
FROM vehicle_service_types vst
JOIN service_areas area ON area.city_code = vst.city_code AND area.status = 1
WHERE vst.service_type_code = 'rt6_comfort'
  AND NOT EXISTS (
      SELECT 1 FROM pricing_rules existing
      WHERE existing.service_type_id = vst.id AND existing.version = '2026-10-v1'
  );

CREATE TABLE IF NOT EXISTS fare_quote_snapshots (
    id BIGSERIAL PRIMARY KEY,
    quote_token VARCHAR(80) NOT NULL UNIQUE,
    passenger_id BIGINT REFERENCES passengers(id),
    service_area_id BIGINT NOT NULL REFERENCES service_areas(id),
    city_code VARCHAR(32) NOT NULL,
    ride_mode VARCHAR(16) NOT NULL CHECK (ride_mode IN ('NOW', 'SCHEDULED')),
    scheduled_at TIMESTAMPTZ,
    origin_place_id VARCHAR(128) NOT NULL,
    origin_name VARCHAR(128) NOT NULL,
    origin_address VARCHAR(255) NOT NULL,
    origin_longitude NUMERIC(10, 7) NOT NULL,
    origin_latitude NUMERIC(10, 7) NOT NULL,
    destination_place_id VARCHAR(128) NOT NULL,
    destination_name VARCHAR(128) NOT NULL,
    destination_address VARCHAR(255) NOT NULL,
    destination_longitude NUMERIC(10, 7) NOT NULL,
    destination_latitude NUMERIC(10, 7) NOT NULL,
    distance_meters INTEGER NOT NULL CHECK (distance_meters > 0),
    estimated_duration_seconds INTEGER NOT NULL CHECK (estimated_duration_seconds > 0),
    polyline TEXT NOT NULL DEFAULT '',
    passenger_coupon_id BIGINT REFERENCES passenger_coupons(id),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_fare_quote_schedule
        CHECK ((ride_mode = 'NOW' AND scheduled_at IS NULL) OR
               (ride_mode = 'SCHEDULED' AND scheduled_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_fare_quote_snapshots_expiry
    ON fare_quote_snapshots (expires_at);

CREATE TABLE IF NOT EXISTS fare_quote_options (
    id BIGSERIAL PRIMARY KEY,
    quote_id BIGINT NOT NULL REFERENCES fare_quote_snapshots(id) ON DELETE CASCADE,
    service_type_id BIGINT NOT NULL REFERENCES vehicle_service_types(id),
    pricing_rule_id BIGINT NOT NULL REFERENCES pricing_rules(id),
    pickup_eta_seconds INTEGER NOT NULL CHECK (pickup_eta_seconds >= 0),
    original_amount NUMERIC(10, 2) NOT NULL CHECK (original_amount >= 0),
    discount_amount NUMERIC(10, 2) NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    payable_amount NUMERIC(10, 2) NOT NULL CHECK (payable_amount >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_fare_quote_options_quote_service UNIQUE (quote_id, service_type_id),
    CONSTRAINT ck_fare_quote_options_amount
        CHECK (payable_amount = original_amount - discount_amount)
);

ALTER TABLE trip_orders
    ADD COLUMN IF NOT EXISTS estimated_distance_m INTEGER,
    ADD COLUMN IF NOT EXISTS estimated_duration_s INTEGER,
    ADD COLUMN IF NOT EXISTS actual_distance_m INTEGER,
    ADD COLUMN IF NOT EXISTS actual_duration_s INTEGER,
    ADD COLUMN IF NOT EXISTS quote_id BIGINT REFERENCES fare_quote_snapshots(id),
    ADD COLUMN IF NOT EXISTS service_type_id BIGINT REFERENCES vehicle_service_types(id),
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128),
    ADD COLUMN IF NOT EXISTS ride_mode VARCHAR(16) NOT NULL DEFAULT 'NOW'
        CHECK (ride_mode IN ('NOW', 'SCHEDULED')),
    ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS passenger_count SMALLINT NOT NULL DEFAULT 1
        CHECK (passenger_count > 0),
    ADD COLUMN IF NOT EXISTS contact_phone_ciphertext BYTEA,
    ADD COLUMN IF NOT EXISTS contact_phone_masked VARCHAR(32),
    ADD COLUMN IF NOT EXISTS user_agreement_version VARCHAR(64),
    ADD COLUMN IF NOT EXISTS pricing_rule_version VARCHAR(64),
    ADD COLUMN IF NOT EXISTS boarding_code_hash VARCHAR(128),
    ADD COLUMN IF NOT EXISTS boarding_code_suffix VARCHAR(8);

CREATE UNIQUE INDEX IF NOT EXISTS uq_trip_orders_passenger_idempotency
    ON trip_orders (passenger_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

ALTER TABLE order_fee_items
    ADD COLUMN IF NOT EXISTS item_type VARCHAR(32),
    ADD COLUMN IF NOT EXISTS item_name VARCHAR(128),
    ADD COLUMN IF NOT EXISTS estimated_amount NUMERIC(10, 2),
    ADD COLUMN IF NOT EXISTS final_amount NUMERIC(10, 2);

CREATE INDEX IF NOT EXISTS idx_order_fee_items_order ON order_fee_items (order_id);

CREATE TABLE IF NOT EXISTS order_commands (
    id BIGSERIAL PRIMARY KEY,
    command_no VARCHAR(40) NOT NULL UNIQUE,
    order_id BIGINT NOT NULL REFERENCES trip_orders(id),
    passenger_id BIGINT NOT NULL REFERENCES passengers(id),
    command_type VARCHAR(32) NOT NULL
        CHECK (command_type IN ('CANCEL', 'UNLOCK_AND_START', 'REQUEST_PULL_OVER')),
    idempotency_key VARCHAR(128) NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0 CHECK (status IN (0, 1, 2)),
    result_code VARCHAR(64),
    reason_code VARCHAR(64),
    client_longitude NUMERIC(10, 7),
    client_latitude NUMERIC(10, 7),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    CONSTRAINT uq_order_commands_passenger_idempotency
        UNIQUE (passenger_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_order_commands_order_created
    ON order_commands (order_id, created_at);

COMMIT;
