package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// Create 保存新区域，并使用现有雪花 ID 生成器分配主键。
func (repository *ServiceAreaRepository) Create(ctx context.Context, area biz.ManagedServiceArea) (biz.ManagedServiceArea, error) {
	if repository.data.dispatchIDGenerator == nil {
		return biz.ManagedServiceArea{}, errors.New("service area ID generator is unavailable")
	}
	area.ID = repository.data.dispatchIDGenerator.New()
	_, err := repository.data.pool.Exec(ctx, `INSERT INTO public.service_areas
		(id, area_code, area_name, city_code, city_name, boundary_geojson, service_notice, status)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, NULLIF($7, ''), $8)`,
		area.ID, area.AreaCode, area.AreaName, area.CityCode, area.CityName, area.BoundaryGeoJSON, area.ServiceNotice, area.Status)
	if err != nil {
		return biz.ManagedServiceArea{}, mapServiceAreaWriteError(err, "create service area")
	}
	return area, nil
}

// List 返回确定排序的分页区域资料及总数。
func (repository *ServiceAreaRepository) List(ctx context.Context, query biz.ServiceAreaListQuery) ([]biz.ManagedServiceArea, int64, error) {
	where, arguments := serviceAreaFilters(query)
	var total int64
	if err := repository.data.pool.QueryRow(ctx, "SELECT count(*) FROM public.service_areas "+where, arguments...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count service areas: %w", err)
	}
	arguments = append(arguments, query.PageSize, (query.Page-1)*query.PageSize)
	limitIndex := len(arguments) - 1
	rows, err := repository.data.pool.Query(ctx, managedServiceAreaSelectSQL()+" "+where+
		fmt.Sprintf(" ORDER BY city_code ASC, area_code ASC, id ASC LIMIT $%d OFFSET $%d", limitIndex, limitIndex+1), arguments...)
	if err != nil {
		return nil, 0, fmt.Errorf("list service areas: %w", err)
	}
	defer rows.Close()
	areas := make([]biz.ManagedServiceArea, 0)
	for rows.Next() {
		area, err := scanManagedServiceArea(rows)
		if err != nil {
			return nil, 0, err
		}
		areas = append(areas, area)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate service areas: %w", err)
	}
	return areas, total, nil
}

// GetDetail 返回区域资料与全部时段，供后台维护页面编辑。
func (repository *ServiceAreaRepository) GetDetail(ctx context.Context, id int64) (biz.ManagedServiceAreaDetail, error) {
	area, err := repository.getManagedArea(ctx, id)
	if err != nil {
		return biz.ManagedServiceAreaDetail{}, err
	}
	schedules, err := repository.listManagedSchedules(ctx, id)
	if err != nil {
		return biz.ManagedServiceAreaDetail{}, err
	}
	return biz.ManagedServiceAreaDetail{ServiceArea: area, Schedules: schedules}, nil
}

// Update 仅持久化外部可编辑字段，区域编码和状态均由各自专用操作维护。
func (repository *ServiceAreaRepository) Update(ctx context.Context, area biz.ManagedServiceArea) (biz.ManagedServiceArea, error) {
	result, err := repository.data.pool.Exec(ctx, `UPDATE public.service_areas SET
		area_name = $1, city_code = $2, city_name = $3, boundary_geojson = $4::jsonb,
		service_notice = NULLIF($5, ''), updated_at = CURRENT_TIMESTAMP WHERE id = $6`,
		area.AreaName, area.CityCode, area.CityName, area.BoundaryGeoJSON, area.ServiceNotice, area.ID)
	if err != nil {
		return biz.ManagedServiceArea{}, mapServiceAreaWriteError(err, "update service area")
	}
	if result.RowsAffected() == 0 {
		return biz.ManagedServiceArea{}, biz.ErrServiceAreaNotFound
	}
	return area, nil
}

// ChangeStatus 修改区域总开关后返回完整资料。
func (repository *ServiceAreaRepository) ChangeStatus(ctx context.Context, id int64, status int16) (biz.ManagedServiceArea, error) {
	result, err := repository.data.pool.Exec(ctx,
		"UPDATE public.service_areas SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	if err != nil {
		return biz.ManagedServiceArea{}, fmt.Errorf("change service area status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return biz.ManagedServiceArea{}, biz.ErrServiceAreaNotFound
	}
	return repository.getManagedArea(ctx, id)
}

// ReplaceSchedules 在单一事务中替换一个区域的全部时段，任何插入失败都会恢复旧规则。
func (repository *ServiceAreaRepository) ReplaceSchedules(ctx context.Context, id int64, schedules []biz.ManagedServiceAreaSchedule) error {
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin replace service-area schedules: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := ensureServiceAreaExists(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM public.service_area_schedules WHERE service_area_id = $1", id); err != nil {
		return fmt.Errorf("delete service-area schedules: %w", err)
	}
	for _, schedule := range schedules {
		if err := repository.insertManagedSchedule(ctx, tx, id, schedule); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit replace service-area schedules: %w", err)
	}
	return nil
}

func (repository *ServiceAreaRepository) getManagedArea(ctx context.Context, id int64) (biz.ManagedServiceArea, error) {
	area, err := scanManagedServiceArea(repository.data.pool.QueryRow(ctx, managedServiceAreaSelectSQL()+" WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ManagedServiceArea{}, biz.ErrServiceAreaNotFound
	}
	if err != nil {
		return biz.ManagedServiceArea{}, fmt.Errorf("get managed service area: %w", err)
	}
	return area, nil
}

func (repository *ServiceAreaRepository) listManagedSchedules(ctx context.Context, id int64) ([]biz.ManagedServiceAreaSchedule, error) {
	rows, err := repository.data.pool.Query(ctx, `SELECT day_of_week, special_date, start_time, end_time, is_enabled,
		COALESCE(notice, '') FROM public.service_area_schedules WHERE service_area_id = $1
		ORDER BY special_date NULLS FIRST, day_of_week ASC, start_time ASC, id ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("list managed service-area schedules: %w", err)
	}
	defer rows.Close()
	schedules := make([]biz.ManagedServiceAreaSchedule, 0)
	for rows.Next() {
		schedule, err := scanManagedServiceAreaSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed service-area schedules: %w", err)
	}
	return schedules, nil
}

func (repository *ServiceAreaRepository) insertManagedSchedule(ctx context.Context, tx pgx.Tx, areaID int64, schedule biz.ManagedServiceAreaSchedule) error {
	if repository.data.dispatchIDGenerator == nil {
		return errors.New("service area schedule ID generator is unavailable")
	}
	specialDate, err := managedScheduleDate(schedule.SpecialDate)
	if err != nil {
		return err
	}
	var dayOfWeek *int
	if schedule.DayOfWeek != 0 {
		dayOfWeek = &schedule.DayOfWeek
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.service_area_schedules
		(id, service_area_id, day_of_week, special_date, start_time, end_time, is_enabled, notice)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))`, repository.data.dispatchIDGenerator.New(), areaID,
		dayOfWeek, specialDate, pgTime(schedule.StartMinute), pgTime(schedule.EndMinute), schedule.IsEnabled, schedule.Notice)
	if err != nil {
		return fmt.Errorf("insert service-area schedule: %w", err)
	}
	return nil
}

func serviceAreaFilters(query biz.ServiceAreaListQuery) (string, []any) {
	filters, arguments := []string{"WHERE TRUE"}, make([]any, 0, 2)
	if query.CityCode != "" {
		arguments = append(arguments, query.CityCode)
		filters = append(filters, fmt.Sprintf("city_code = $%d", len(arguments)))
	}
	if query.Status != nil {
		arguments = append(arguments, *query.Status)
		filters = append(filters, fmt.Sprintf("status = $%d", len(arguments)))
	}
	return strings.Join(filters, " AND "), arguments
}

func managedServiceAreaSelectSQL() string {
	return "SELECT id, area_code, area_name, city_code, city_name, boundary_geojson::text, COALESCE(service_notice, ''), status FROM public.service_areas"
}

func scanManagedServiceArea(row pgx.Row) (biz.ManagedServiceArea, error) {
	var area biz.ManagedServiceArea
	err := row.Scan(&area.ID, &area.AreaCode, &area.AreaName, &area.CityCode, &area.CityName,
		&area.BoundaryGeoJSON, &area.ServiceNotice, &area.Status)
	return area, err
}

func scanManagedServiceAreaSchedule(rows pgx.Rows) (biz.ManagedServiceAreaSchedule, error) {
	var weekday *int
	var specialDate *time.Time
	var start, end pgtype.Time
	var schedule biz.ManagedServiceAreaSchedule
	err := rows.Scan(&weekday, &specialDate, &start, &end, &schedule.IsEnabled, &schedule.Notice)
	if err != nil {
		return biz.ManagedServiceAreaSchedule{}, fmt.Errorf("scan managed service-area schedule: %w", err)
	}
	if weekday != nil {
		schedule.DayOfWeek = *weekday
	}
	if specialDate != nil {
		schedule.SpecialDate = specialDate.Format("2006-01-02")
	}
	schedule.StartMinute, schedule.EndMinute = pgMinute(start), pgMinute(end)
	return schedule, nil
}

func ensureServiceAreaExists(ctx context.Context, tx pgx.Tx, id int64) error {
	var found int
	err := tx.QueryRow(ctx, "SELECT 1 FROM public.service_areas WHERE id = $1", id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ErrServiceAreaNotFound
	}
	if err != nil {
		return fmt.Errorf("find service area for schedules: %w", err)
	}
	return nil
}

func managedScheduleDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("parse service-area schedule date: %w", err)
	}
	return &parsed, nil
}

func pgTime(minute int) pgtype.Time {
	return pgtype.Time{Microseconds: int64(minute) * int64(time.Minute/time.Microsecond), Valid: true}
}

func pgMinute(value pgtype.Time) int {
	return int(value.Microseconds / int64(time.Minute/time.Microsecond))
}

func mapServiceAreaWriteError(err error, action string) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "service_areas_area_code_key" {
		return biz.ErrServiceAreaCodeConflict
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ biz.ServiceAreaAdminRepository = (*ServiceAreaRepository)(nil)
