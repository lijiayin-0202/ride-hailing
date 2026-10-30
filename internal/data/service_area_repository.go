package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// 保留数据层别名，兼容既有调用；服务层统一识别领域错误。
var ErrServiceAreaNotFound = biz.ErrServiceAreaNotFound

// ServiceAreaRepository 从 PostgreSQL 读取运营区域及其服务时段。
type ServiceAreaRepository struct {
	data *Data
}

// NewServiceAreaRepository 创建运营区域仓储。
func NewServiceAreaRepository(data *Data) *ServiceAreaRepository {
	return &ServiceAreaRepository{data: data}
}

// Get 读取运营区域的最小领域事实。
func (repository *ServiceAreaRepository) Get(ctx context.Context, id int64) (biz.ServiceArea, error) {
	var area biz.ServiceArea
	err := repository.data.pool.QueryRow(ctx, `
		SELECT id, status FROM public.service_areas WHERE id = $1`, id,
	).Scan(&area.ID, &area.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.ServiceArea{}, ErrServiceAreaNotFound
	}
	if err != nil {
		return biz.ServiceArea{}, fmt.Errorf("get service area: %w", err)
	}
	return area, nil
}

// ListSchedules 读取前一天与目标日期适用的常规及特殊日期服务时段。
func (repository *ServiceAreaRepository) ListSchedules(ctx context.Context, areaID int64, from, to time.Time) ([]biz.ServiceAreaSchedule, error) {
	// 数据库只保存时钟值，查询与转换统一以中国时区日期解释，防止 UTC 跨日错配。
	fromDate := localDate(from)
	toDate := localDate(to)
	rows, err := repository.data.pool.Query(ctx, `
		SELECT day_of_week, special_date, start_time, end_time, is_enabled
		FROM public.service_area_schedules
		WHERE service_area_id = $1
			AND (
				(special_date BETWEEN $2::date AND $3::date)
				OR (special_date IS NULL AND day_of_week IN ($4, $5))
			)
		ORDER BY special_date NULLS FIRST, day_of_week ASC, start_time ASC`,
		areaID, fromDate, toDate, isoWeekday(fromDate), isoWeekday(toDate))
	if err != nil {
		return nil, fmt.Errorf("list service-area schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]biz.ServiceAreaSchedule, 0)
	for rows.Next() {
		schedule, err := scanServiceAreaSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service-area schedules: %w", err)
	}
	return schedules, nil
}

func scanServiceAreaSchedule(rows pgx.Rows) (biz.ServiceAreaSchedule, error) {
	var weekday *int
	var specialDate *time.Time
	var start, end pgtype.Time
	var schedule biz.ServiceAreaSchedule
	err := rows.Scan(&weekday, &specialDate, &start, &end, &schedule.IsEnabled)
	if err != nil {
		return biz.ServiceAreaSchedule{}, fmt.Errorf("scan service-area schedule: %w", err)
	}
	if weekday != nil {
		schedule.DayOfWeek = *weekday
	}
	if specialDate != nil {
		schedule.SpecialDate = localDate(*specialDate)
	}
	schedule.StartMinute = int(start.Microseconds / int64(time.Minute/time.Microsecond))
	schedule.EndMinute = int(end.Microseconds / int64(time.Minute/time.Microsecond))
	return schedule, nil
}

func localDate(value time.Time) time.Time {
	location, _ := time.LoadLocation("Asia/Shanghai")
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func isoWeekday(value time.Time) int {
	return int(value.Weekday()+6)%7 + 1
}

var _ biz.ServiceAreaRepository = (*ServiceAreaRepository)(nil)
