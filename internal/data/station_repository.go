package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var (
	ErrStationNotFound     = errors.New("站点不存在")
	ErrStationCodeConflict = errors.New("站点编码冲突")
	ErrStationNameConflict = errors.New("运营区域内站点名称冲突")
)

// StationRepository 通过 PostgreSQL 维护站点资料。
type StationRepository struct {
	data *Data
}

// NewStationRepository 创建站点仓储。
func NewStationRepository(data *Data) *StationRepository {
	return &StationRepository{data: data}
}

// Create 保存新站点并分配应用侧雪花 ID。
func (repository *StationRepository) Create(ctx context.Context, station biz.Station) (biz.Station, error) {
	if repository.data.dispatchIDGenerator == nil {
		return biz.Station{}, errors.New("station ID generator is unavailable")
	}
	// 站点与派单共用应用侧 BIGINT ID 生成器，避免由数据库序列造成多服务 ID 冲突。
	station.ID = repository.data.dispatchIDGenerator.New()
	_, err := repository.data.pool.Exec(ctx, stationInsertSQL(), stationValues(station)...)
	if err != nil {
		return biz.Station{}, mapStationWriteError(err, "create station")
	}
	return station, nil
}

// Get 按 ID 读取站点。
func (repository *StationRepository) Get(ctx context.Context, id int64) (biz.Station, error) {
	station, err := scanStation(repository.data.pool.QueryRow(ctx,
		stationSelectSQL()+" WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.Station{}, ErrStationNotFound
	}
	if err != nil {
		return biz.Station{}, fmt.Errorf("get station: %w", err)
	}
	return station, nil
}

// Update 更新站点的可编辑资料。
func (repository *StationRepository) Update(ctx context.Context, station biz.Station) (biz.Station, error) {
	result, err := repository.data.pool.Exec(ctx, stationUpdateSQL(), stationUpdateValues(station)...)
	if err != nil {
		return biz.Station{}, mapStationWriteError(err, "update station")
	}
	if result.RowsAffected() == 0 {
		return biz.Station{}, ErrStationNotFound
	}
	return station, nil
}

// ChangeStatus 启用或停用站点。
func (repository *StationRepository) ChangeStatus(ctx context.Context, id int64, status int16) (biz.Station, error) {
	result, err := repository.data.pool.Exec(ctx,
		"UPDATE public.stations SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	if err != nil {
		return biz.Station{}, fmt.Errorf("change station status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return biz.Station{}, ErrStationNotFound
	}
	return repository.Get(ctx, id)
}

// List 返回某区域的站点，区域 ID 为零时返回所有站点。
func (repository *StationRepository) List(ctx context.Context, areaID int64) ([]biz.Station, error) {
	query := stationSelectSQL() + " ORDER BY service_area_id ASC, station_name ASC, id ASC"
	arguments := []any{}
	if areaID > 0 {
		query = stationSelectSQL() + " WHERE service_area_id = $1 ORDER BY station_name ASC, id ASC"
		arguments = append(arguments, areaID)
	}
	return repository.list(ctx, query, arguments...)
}

// ListAvailable 返回区域中可用于指定上下车用途的站点。
func (repository *StationRepository) ListAvailable(ctx context.Context, areaID int64, direction int16) ([]biz.Station, error) {
	// 先在 SQL 层过滤区域、状态和方向，避免读取大量不可用站点后再在内存裁剪。
	query := stationSelectWithAliasSQL() + " JOIN public.service_areas AS area ON area.id = station.service_area_id " +
		"WHERE station.service_area_id = $1 AND area.status = 1 AND station.status = 1 " +
		"AND station.station_type IN (1, 2) AND station.service_direction IN ($2, 3) " +
		"ORDER BY station.station_type ASC, station.station_name ASC, station.id ASC"
	return repository.list(ctx, query, areaID, direction)
}

func (repository *StationRepository) list(ctx context.Context, query string, arguments ...any) ([]biz.Station, error) {
	rows, err := repository.data.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list stations: %w", err)
	}
	defer rows.Close()
	stations := make([]biz.Station, 0)
	for rows.Next() {
		station, err := scanStation(rows)
		if err != nil {
			return nil, err
		}
		stations = append(stations, station)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stations: %w", err)
	}
	return stations, nil
}

func stationSelectSQL() string {
	return "SELECT id, service_area_id, station_code, station_name, COALESCE(alias_name, ''), address, longitude, latitude, station_type, service_direction, COALESCE(direction_hint, ''), status FROM public.stations"
}

func stationSelectWithAliasSQL() string {
	return "SELECT station.id, station.service_area_id, station.station_code, station.station_name, COALESCE(station.alias_name, ''), station.address, station.longitude, station.latitude, station.station_type, station.service_direction, COALESCE(station.direction_hint, ''), station.status FROM public.stations AS station"
}

func stationInsertSQL() string {
	return "INSERT INTO public.stations (id, service_area_id, station_code, station_name, alias_name, address, longitude, latitude, station_type, service_direction, direction_hint, status) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10, NULLIF($11, ''), $12)"
}

func stationUpdateSQL() string {
	return "UPDATE public.stations SET station_name = $1, alias_name = NULLIF($2, ''), address = $3, longitude = $4, latitude = $5, station_type = $6, service_direction = $7, direction_hint = NULLIF($8, ''), updated_at = CURRENT_TIMESTAMP WHERE id = $9"
}

func stationValues(station biz.Station) []any {
	return []any{station.ID, station.ServiceAreaID, station.StationCode, station.StationName, station.AliasName, station.Address, station.Longitude, station.Latitude, station.StationType, station.ServiceDirection, station.DirectionHint, station.Status}
}

func stationUpdateValues(station biz.Station) []any {
	return []any{station.StationName, station.AliasName, station.Address, station.Longitude, station.Latitude, station.StationType, station.ServiceDirection, station.DirectionHint, station.ID}
}

func scanStation(row pgx.Row) (biz.Station, error) {
	var station biz.Station
	err := row.Scan(&station.ID, &station.ServiceAreaID, &station.StationCode, &station.StationName,
		&station.AliasName, &station.Address, &station.Longitude, &station.Latitude, &station.StationType,
		&station.ServiceDirection, &station.DirectionHint, &station.Status)
	if err != nil {
		return biz.Station{}, err
	}
	return station, nil
}

func mapStationWriteError(err error, action string) error {
	// 将数据库唯一约束映射为稳定的业务错误，接口层无需识别 PostgreSQL 错误码。
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.ConstraintName {
		case "stations_station_code_key":
			return ErrStationCodeConflict
		case "stations_service_area_name_key":
			return ErrStationNameConflict
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ biz.StationRepository = (*StationRepository)(nil)
