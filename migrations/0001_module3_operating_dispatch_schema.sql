-- 模块 3 的基础运营与调度事实表；所有主键由应用侧雪花 ID 生成，避免依赖单库序列。
BEGIN;

CREATE TABLE public.service_areas (
    id BIGINT PRIMARY KEY,
    area_code VARCHAR(40) NOT NULL,
    area_name VARCHAR(100) NOT NULL,
    city_code VARCHAR(20) NOT NULL,
    city_name VARCHAR(60) NOT NULL,
    boundary_geojson JSONB NOT NULL,
    service_notice VARCHAR(500),
    status SMALLINT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT service_areas_area_code_key UNIQUE (area_code),
    -- 区域总开关优先于任意时段规则，支持临时关闭、运营和停运三种状态。
    CONSTRAINT service_areas_status_check CHECK (status IN (0, 1, 2))
);

CREATE INDEX idx_service_areas_status ON public.service_areas (status);

CREATE TABLE public.service_area_schedules (
    id BIGINT PRIMARY KEY,
    service_area_id BIGINT NOT NULL,
    day_of_week SMALLINT,
    start_time TIME(6) NOT NULL,
    end_time TIME(6) NOT NULL,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    special_date DATE,
    notice VARCHAR(300),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_service_area_schedules_area
        FOREIGN KEY (service_area_id) REFERENCES public.service_areas (id),
    CONSTRAINT service_area_schedules_day_check
        CHECK (day_of_week IS NULL OR day_of_week BETWEEN 1 AND 7),
    -- 一条规则只能是“星期常规”或“特殊日期”，避免两套优先级混在同一行。
    CONSTRAINT service_area_schedules_rule_type_check
        CHECK (
            (special_date IS NULL AND day_of_week IS NOT NULL)
            OR (special_date IS NOT NULL AND day_of_week IS NULL)
        )
);

CREATE UNIQUE INDEX ux_service_area_schedules_weekly_period
    ON public.service_area_schedules (service_area_id, day_of_week, start_time, end_time)
    WHERE special_date IS NULL;

CREATE UNIQUE INDEX ux_service_area_schedules_special_date_period
    ON public.service_area_schedules (service_area_id, special_date, start_time, end_time)
    WHERE special_date IS NOT NULL;

CREATE INDEX idx_service_area_schedules_area_enabled
    ON public.service_area_schedules (service_area_id, is_enabled);

CREATE TABLE public.stations (
    id BIGINT PRIMARY KEY,
    service_area_id BIGINT NOT NULL,
    station_code VARCHAR(40) NOT NULL,
    station_name VARCHAR(120) NOT NULL,
    alias_name VARCHAR(120),
    address VARCHAR(300) NOT NULL,
    longitude NUMERIC(10, 7) NOT NULL,
    latitude NUMERIC(9, 7) NOT NULL,
    station_type SMALLINT NOT NULL,
    direction_hint VARCHAR(300),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_stations_service_area
        FOREIGN KEY (service_area_id) REFERENCES public.service_areas (id),
    -- 编码作为跨模块稳定定位标识，创建后在领域层不可变。
    CONSTRAINT stations_station_code_key UNIQUE (station_code),
    CONSTRAINT stations_station_type_check CHECK (station_type IN (1, 2, 3)),
    CONSTRAINT stations_longitude_check CHECK (longitude BETWEEN -180 AND 180),
    CONSTRAINT stations_latitude_check CHECK (latitude BETWEEN -90 AND 90)
);

CREATE INDEX idx_stations_service_area ON public.stations (service_area_id);
CREATE INDEX idx_stations_service_area_coordinate
    ON public.stations (service_area_id, longitude, latitude);

CREATE TABLE public.vehicles (
    id BIGINT PRIMARY KEY,
    vehicle_no VARCHAR(40) NOT NULL,
    vin VARCHAR(40) NOT NULL,
    plate_no VARCHAR(20) NOT NULL,
    vehicle_model VARCHAR(80) NOT NULL,
    seat_count INTEGER NOT NULL,
    service_area_id BIGINT NOT NULL,
    operation_status SMALLINT NOT NULL,
    online_status SMALLINT NOT NULL,
    battery_percent NUMERIC(5, 2),
    current_longitude NUMERIC(10, 7),
    current_latitude NUMERIC(9, 7),
    software_version VARCHAR(80),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_vehicles_service_area
        FOREIGN KEY (service_area_id) REFERENCES public.service_areas (id),
    CONSTRAINT vehicles_vehicle_no_key UNIQUE (vehicle_no),
    CONSTRAINT vehicles_vin_key UNIQUE (vin),
    CONSTRAINT vehicles_plate_no_key UNIQUE (plate_no),
    CONSTRAINT vehicles_seat_count_check CHECK (seat_count > 0),
    CONSTRAINT vehicles_operation_status_check CHECK (operation_status IN (0, 1, 2, 3, 4, 5)),
    CONSTRAINT vehicles_online_status_check CHECK (online_status IN (0, 1)),
    CONSTRAINT vehicles_battery_percent_check
        CHECK (battery_percent IS NULL OR battery_percent BETWEEN 0 AND 100),
    CONSTRAINT vehicles_longitude_check
        CHECK (current_longitude IS NULL OR current_longitude BETWEEN -180 AND 180),
    CONSTRAINT vehicles_latitude_check
        CHECK (current_latitude IS NULL OR current_latitude BETWEEN -90 AND 90)
);

-- 档案层索引服务于按运营区域筛选候选车辆；最终资格仍须检查最新状态日志。
CREATE INDEX idx_vehicles_dispatch_candidate
    ON public.vehicles (service_area_id, online_status, operation_status);

CREATE TABLE public.vehicle_status_logs (
    id BIGINT PRIMARY KEY,
    vehicle_id BIGINT NOT NULL,
    online_status SMALLINT NOT NULL,
    operation_status SMALLINT NOT NULL,
    battery_percent NUMERIC(5, 2),
    current_longitude NUMERIC(10, 7),
    current_latitude NUMERIC(9, 7),
    fault_code VARCHAR(80),
    is_accepting_dispatch BOOLEAN NOT NULL DEFAULT FALSE,
    reported_at TIMESTAMPTZ(6) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_vehicle_status_logs_vehicle
        FOREIGN KEY (vehicle_id) REFERENCES public.vehicles (id),
    CONSTRAINT vehicle_status_logs_online_status_check CHECK (online_status IN (0, 1)),
    CONSTRAINT vehicle_status_logs_operation_status_check CHECK (operation_status IN (0, 1, 2, 3, 4, 5)),
    CONSTRAINT vehicle_status_logs_battery_percent_check
        CHECK (battery_percent IS NULL OR battery_percent BETWEEN 0 AND 100),
    CONSTRAINT vehicle_status_logs_longitude_check
        CHECK (current_longitude IS NULL OR current_longitude BETWEEN -180 AND 180),
    CONSTRAINT vehicle_status_logs_latitude_check
        CHECK (current_latitude IS NULL OR current_latitude BETWEEN -90 AND 90)
);

-- 每辆车按上报时间倒序读取最新事实，ID 用于处理相同上报时间的稳定兜底。
CREATE INDEX idx_vehicle_status_logs_vehicle_reported_at
    ON public.vehicle_status_logs (vehicle_id, reported_at DESC);

CREATE TABLE public.dispatch_records (
    id BIGINT PRIMARY KEY,
    dispatch_no VARCHAR(50) NOT NULL,
    order_id BIGINT NOT NULL,
    vehicle_id BIGINT NOT NULL,
    attempt_no SMALLINT NOT NULL,
    dispatch_strategy VARCHAR(80) NOT NULL,
    candidate_score NUMERIC(12, 4),
    estimated_pickup_distance_km NUMERIC(10, 2),
    estimated_arrival_min INTEGER,
    status SMALLINT NOT NULL,
    failure_reason VARCHAR(500),
    dispatched_at TIMESTAMPTZ(6),
    responded_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_dispatch_records_vehicle
        FOREIGN KEY (vehicle_id) REFERENCES public.vehicles (id),
    CONSTRAINT dispatch_records_dispatch_no_key UNIQUE (dispatch_no),
    -- 同一订单的尝试序号不可重复，三次上限由应用层与该约束共同保护。
    CONSTRAINT dispatch_records_order_attempt_key UNIQUE (order_id, attempt_no),
    CONSTRAINT dispatch_records_attempt_no_check CHECK (attempt_no > 0),
    CONSTRAINT dispatch_records_status_check CHECK (status IN (0, 1, 2, 3, 4)),
    CONSTRAINT dispatch_records_candidate_score_check
        CHECK (candidate_score IS NULL OR candidate_score >= 0),
    CONSTRAINT dispatch_records_pickup_distance_check
        CHECK (estimated_pickup_distance_km IS NULL OR estimated_pickup_distance_km >= 0),
    CONSTRAINT dispatch_records_arrival_min_check
        CHECK (estimated_arrival_min IS NULL OR estimated_arrival_min >= 0),
    CONSTRAINT dispatch_records_dispatched_at_check
        CHECK ((status = 0 AND dispatched_at IS NULL) OR (status IN (1, 2, 3, 4) AND dispatched_at IS NOT NULL)),
    CONSTRAINT dispatch_records_responded_at_check
        CHECK (responded_at IS NULL OR (dispatched_at IS NOT NULL AND responded_at >= dispatched_at))
);

-- 任意订单同时最多存在一条 Created/Pending 派单，重复 assign 可安全复用。
CREATE UNIQUE INDEX ux_dispatch_records_order_active
    ON public.dispatch_records (order_id)
    WHERE status IN (0, 1);

CREATE INDEX idx_dispatch_records_order_created_at
    ON public.dispatch_records (order_id, created_at DESC);

CREATE INDEX idx_dispatch_records_vehicle_created_at
    ON public.dispatch_records (vehicle_id, created_at DESC);

-- 超时工作者只扫描 Pending 记录，部分索引避免读取大量终态历史。
CREATE INDEX idx_dispatch_records_timeout_scan
    ON public.dispatch_records (status, dispatched_at)
    WHERE status = 1;

COMMIT;
