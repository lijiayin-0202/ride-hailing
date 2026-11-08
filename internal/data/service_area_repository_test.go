package data

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestServiceAreaRepositoryGetReadsAreaStatus(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	areaID := -time.Now().UnixNano()
	seedServiceArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)

	repository := NewServiceAreaRepository(&Data{pool: pool})
	area, err := repository.Get(ctx, areaID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if area.ID != areaID || area.Status != biz.ServiceAreaStatusOperating {
		t.Errorf("Get() = %#v, want ID %d and operating status", area, areaID)
	}
}

func TestServiceAreaRepositoryGetReturnsNotFoundError(t *testing.T) {
	pool := openModule3TestPool(t)
	repository := NewServiceAreaRepository(&Data{pool: pool})

	_, err := repository.Get(context.Background(), -time.Now().UnixNano())
	if !errors.Is(err, ErrServiceAreaNotFound) {
		t.Errorf("Get() error = %v, want ErrServiceAreaNotFound", err)
	}
}

func TestServiceAreaRepositoryListSchedulesReadsTwoDateScopes(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	areaID := -time.Now().UnixNano()
	seedServiceArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	target := time.Date(2026, time.October, 29, 0, 0, 0, 0, location)
	seedSchedulesForScope(t, ctx, pool, areaID, target)

	repository := NewServiceAreaRepository(&Data{pool: pool})
	schedules, err := repository.ListSchedules(ctx, areaID, target.AddDate(0, 0, -1), target)
	if err != nil {
		t.Fatalf("ListSchedules() error = %v", err)
	}
	if len(schedules) != 4 {
		t.Fatalf("ListSchedules() count = %d, want 4; schedules = %#v", len(schedules), schedules)
	}
	assertScheduleScope(t, schedules, target)
}

func seedServiceArea(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID int64, status int16) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_area_schedules WHERE service_area_id = $1", areaID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", areaID)
	})
	_, err := pool.Exec(ctx, `INSERT INTO public.service_areas
		(id, area_code, area_name, city_code, city_name, boundary_geojson, status)
		VALUES ($1, $2, $2, 'test-city', '测试城市', '{}'::jsonb, $3)`,
		areaID, fmt.Sprintf("area-%d", -areaID), status)
	if err != nil {
		t.Fatalf("insert service area: %v", err)
	}
}

func seedSchedulesForScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID int64, target time.Time) {
	t.Helper()
	previous := target.AddDate(0, 0, -1)
	insertSchedule(t, ctx, pool, areaID, 3, nil, "22:00", "02:00", true)
	insertSchedule(t, ctx, pool, areaID, 4, nil, "09:00", "18:00", true)
	insertSchedule(t, ctx, pool, areaID, 0, &previous, "08:00", "10:00", false)
	insertSchedule(t, ctx, pool, areaID, 0, &target, "10:00", "20:00", true)
	insertSchedule(t, ctx, pool, areaID, 5, nil, "09:00", "18:00", true)
	otherDay := target.AddDate(0, 0, 1)
	insertSchedule(t, ctx, pool, areaID, 0, &otherDay, "09:00", "18:00", true)
}

func insertSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID int64, weekday int, specialDate *time.Time, start, end string, enabled bool) {
	t.Helper()
	var dayOfWeek *int
	if weekday != 0 {
		dayOfWeek = &weekday
	}
	_, err := pool.Exec(ctx, `INSERT INTO public.service_area_schedules
		(id, service_area_id, day_of_week, special_date, start_time, end_time, is_enabled)
		VALUES ($1, $2, $3, $4, $5::time, $6::time, $7)`,
		-time.Now().UnixNano(), areaID, dayOfWeek, specialDate, start, end, enabled)
	if err != nil {
		t.Fatalf("insert service-area schedule: %v", err)
	}
}

func assertScheduleScope(t *testing.T, schedules []biz.ServiceAreaSchedule, target time.Time) {
	t.Helper()
	if schedules[0].DayOfWeek != 3 || schedules[0].StartMinute != 1320 || schedules[0].EndMinute != 120 {
		t.Errorf("first schedule = %#v, want Wednesday 22:00-02:00", schedules[0])
	}
	if schedules[1].DayOfWeek != 4 || schedules[1].StartMinute != 540 || schedules[1].EndMinute != 1080 {
		t.Errorf("second schedule = %#v, want Thursday 09:00-18:00", schedules[1])
	}
	if !schedules[2].SpecialDate.Equal(target.AddDate(0, 0, -1)) || schedules[2].IsEnabled {
		t.Errorf("third schedule = %#v, want disabled previous-day special rule", schedules[2])
	}
	if !schedules[3].SpecialDate.Equal(target) || !schedules[3].IsEnabled {
		t.Errorf("fourth schedule = %#v, want enabled target-day special rule", schedules[3])
	}
}
