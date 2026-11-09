BEGIN;
SET search_path TO robotaxi, public;

DROP TABLE IF EXISTS order_commands;

DROP INDEX IF EXISTS uq_trip_orders_passenger_idempotency;

ALTER TABLE trip_orders
    DROP COLUMN IF EXISTS boarding_code_suffix,
    DROP COLUMN IF EXISTS boarding_code_hash,
    DROP COLUMN IF EXISTS pricing_rule_version,
    DROP COLUMN IF EXISTS user_agreement_version,
    DROP COLUMN IF EXISTS contact_phone_masked,
    DROP COLUMN IF EXISTS contact_phone_ciphertext,
    DROP COLUMN IF EXISTS passenger_count,
    DROP COLUMN IF EXISTS scheduled_at,
    DROP COLUMN IF EXISTS ride_mode,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS service_type_id,
    DROP COLUMN IF EXISTS quote_id,
    DROP COLUMN IF EXISTS estimated_distance_m,
    DROP COLUMN IF EXISTS estimated_duration_s,
    DROP COLUMN IF EXISTS actual_distance_m,
    DROP COLUMN IF EXISTS actual_duration_s;

DROP TABLE IF EXISTS fare_quote_options;
DROP TABLE IF EXISTS fare_quote_snapshots;
DROP TABLE IF EXISTS vehicle_service_types;

ALTER TABLE order_fee_items
    DROP COLUMN IF EXISTS final_amount,
    DROP COLUMN IF EXISTS estimated_amount,
    DROP COLUMN IF EXISTS item_name,
    DROP COLUMN IF EXISTS item_type;

ALTER TABLE pricing_rules
    DROP COLUMN IF EXISTS base_distance_m,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS service_type_id;

COMMIT;
