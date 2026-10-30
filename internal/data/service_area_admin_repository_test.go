package data

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestServiceAreaRepositoryReplaceSchedulesRollsBackOnInvalidInsert(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	areaID := -time.Now().UnixNano()
	seedServiceArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)
	insertSchedule(t, ctx, pool, areaID, 1, nil, "09:00", "18:00", true)
	repository := newServiceAreaAdminRepositoryForTest(t, pool)

	before := mustGetManagedSchedules(t, ctx, repository, areaID)
	err := repository.ReplaceSchedules(ctx, areaID, []biz.ManagedServiceAreaSchedule{
		{DayOfWeek: 2, StartMinute: 540, EndMinute: 1080, IsEnabled: true},
		{DayOfWeek: 0, StartMinute: 600, EndMinute: 720, IsEnabled: true},
	})
	if err == nil {
		t.Fatal("ReplaceSchedules() error = nil, want database rule failure")
	}
	after := mustGetManagedSchedules(t, ctx, repository, areaID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("ReplaceSchedules() committed partial data: before = %#v, after = %#v", before, after)
	}
}

func TestServiceAreaRepositoryListsByCityAndStatus(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	repository := newServiceAreaAdminRepositoryForTest(t, pool)
	uniquePart := strconv.FormatInt(time.Now().UnixNano(), 36)
	cityCode := "c" + uniquePart
	area, err := repository.Create(ctx, biz.ManagedServiceArea{AreaCode: "area-" + uniquePart,
		AreaName: "分页测试区域", CityCode: cityCode, CityName: "测试城市", BoundaryGeoJSON: `{}`,
		Status: biz.ServiceAreaStatusOperating})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_area_schedules WHERE service_area_id = $1", area.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", area.ID)
	})
	status := int16(biz.ServiceAreaStatusOperating)

	areas, total, err := repository.List(ctx, biz.ServiceAreaListQuery{
		Page: 1, PageSize: 10, CityCode: cityCode, Status: &status,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || !containsManagedServiceArea(areas, area.ID) {
		t.Fatalf("List() = %#v, total = %d, want created area %d", areas, total, area.ID)
	}
}

func newServiceAreaAdminRepositoryForTest(t *testing.T, pool *pgxpool.Pool) *ServiceAreaRepository {
	t.Helper()
	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("new test ID generator: %v", err)
	}
	return NewServiceAreaRepository(&Data{pool: pool, dispatchIDGenerator: generator})
}

func mustGetManagedSchedules(t *testing.T, ctx context.Context, repository *ServiceAreaRepository, areaID int64) []biz.ManagedServiceAreaSchedule {
	t.Helper()
	detail, err := repository.GetDetail(ctx, areaID)
	if err != nil {
		t.Fatalf("GetDetail() error = %v", err)
	}
	return detail.Schedules
}

func containsManagedServiceArea(areas []biz.ManagedServiceArea, id int64) bool {
	for _, area := range areas {
		if area.ID == id {
			return true
		}
	}
	return false
}
