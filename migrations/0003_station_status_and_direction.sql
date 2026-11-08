-- 将基础站点表补全为可直接支持后台启停及上下车方向筛选的运营模型。
BEGIN;

ALTER TABLE public.stations
    ADD COLUMN status SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN service_direction SMALLINT NOT NULL DEFAULT 3,
    ADD CONSTRAINT stations_status_check CHECK (status IN (0, 1)),
    ADD CONSTRAINT stations_service_direction_check
        CHECK (service_direction IN (1, 2, 3)),
    -- 同一运营区域内显示名称唯一，避免后台维护与乘客选点产生歧义。
    ADD CONSTRAINT stations_service_area_name_key
        UNIQUE (service_area_id, station_name);

COMMIT;
