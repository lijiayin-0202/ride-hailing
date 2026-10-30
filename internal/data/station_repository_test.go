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

func TestStationRepositoryCreateAndGet(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	areaID := -time.Now().UnixNano()
	seedStationArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)
	repository := newStationRepositoryForTest(t, pool)

	created, err := repository.Create(ctx, testStation(areaID, "BJ-YZ-001", "亦庄上车点"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repository.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.StationCode != "BJ-YZ-001" || got.Status != biz.StationStatusEnabled {
		t.Errorf("Get() = %#v, want persisted enabled station", got)
	}
}

func TestStationRepositoryMapsUniqueConflicts(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	areaID := -time.Now().UnixNano()
	seedStationArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)
	repository := newStationRepositoryForTest(t, pool)
	if _, err := repository.Create(ctx, testStation(areaID, "BJ-YZ-002", "同名站点")); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := repository.Create(ctx, testStation(areaID, "BJ-YZ-002", "另一站点")); !errors.Is(err, ErrStationCodeConflict) {
		t.Errorf("duplicate code error = %v, want ErrStationCodeConflict", err)
	}
	if _, err := repository.Create(ctx, testStation(areaID, "BJ-YZ-003", "同名站点")); !errors.Is(err, ErrStationNameConflict) {
		t.Errorf("duplicate name error = %v, want ErrStationNameConflict", err)
	}
}

func TestStationRepositoryListAvailableFiltersFourConditions(t *testing.T) {
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := -time.Now().UnixNano()
	areaID, closedAreaID := base+1, base+2
	seedStationArea(t, ctx, pool, areaID, biz.ServiceAreaStatusOperating)
	seedStationArea(t, ctx, pool, closedAreaID, biz.ServiceAreaStatusTemporarilyClosed)
	seedStationRows(t, ctx, pool, areaID, closedAreaID, base+10)

	repository := newStationRepositoryForTest(t, pool)
	stations, err := repository.ListAvailable(ctx, areaID, biz.StationDirectionPickup)
	if err != nil {
		t.Fatalf("ListAvailable() error = %v", err)
	}
	if len(stations) != 2 || stations[0].ID != base+10 || stations[1].ID != base+11 {
		t.Errorf("ListAvailable() = %#v, want normal and recommended pickup stations", stations)
	}
}

func newStationRepositoryForTest(t *testing.T, pool *pgxpool.Pool) *StationRepository {
	t.Helper()
	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("new test ID generator: %v", err)
	}
	return NewStationRepository(&Data{pool: pool, dispatchIDGenerator: generator})
}

func seedStationArea(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, status int16) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.stations WHERE service_area_id = $1", id)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", id)
	})
	_, err := pool.Exec(ctx, "INSERT INTO public.service_areas (id, area_code, area_name, city_code, city_name, boundary_geojson, status) VALUES ($1, $2, $2, 'test', '测试', '{}'::jsonb, $3)", id, fmt.Sprintf("area-%d", -id), status)
	if err != nil {
		t.Fatalf("insert station area: %v", err)
	}
}

func testStation(areaID int64, code, name string) biz.Station {
	return biz.Station{ServiceAreaID: areaID, StationCode: code, StationName: name,
		Address: "北京示例路 1 号", Longitude: 116.4, Latitude: 39.9,
		StationType: biz.StationTypeNormal, ServiceDirection: biz.StationDirectionBoth,
		Status: biz.StationStatusEnabled}
}

func seedStationRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, areaID, closedAreaID, firstID int64) {
	t.Helper()
	rows := []struct {
		id, area                int64
		kind, direction, status int16
	}{
		{firstID, areaID, 1, 1, 1}, {firstID + 1, areaID, 2, 3, 1},
		{firstID + 2, areaID, 3, 3, 1}, {firstID + 3, areaID, 1, 1, 0},
		{firstID + 4, areaID, 1, 2, 1}, {firstID + 5, closedAreaID, 1, 1, 1},
	}
	for _, row := range rows {
		_, err := pool.Exec(ctx, "INSERT INTO public.stations (id, service_area_id, station_code, station_name, address, longitude, latitude, station_type, service_direction, status) VALUES ($1, $2, $3, $3, '北京', 116.4, 39.9, $4, $5, $6)", row.id, row.area, fmt.Sprintf("station-%d", -row.id), row.kind, row.direction, row.status)
		if err != nil {
			t.Fatalf("insert station row: %v", err)
		}
	}
}
