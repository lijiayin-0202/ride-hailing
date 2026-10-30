package data

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	appserver "github.com/lijayin-0202/radish-ride-service/internal/server"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

func TestServiceAreaToStationAdministrationLifecycle(t *testing.T) {
	pool := openModule3TestPool(t)
	repository := newServiceAreaAdminRepositoryForTest(t, pool)
	areaUsecase := biz.NewServiceAreaAdminUsecase(repository)
	stationUsecase := biz.NewStationUsecase(newStationRepositoryForTest(t, pool), repository)
	server := httptest.NewServer(appserver.NewHTTPServer(&conf.Server{}, log.NewStdLogger(io.Discard),
		service.NewStationService(stationUsecase), service.NewServiceAreaAdminService(areaUsecase), service.NewDispatchService(nil)))
	t.Cleanup(server.Close)

	unique := strconv.FormatInt(time.Now().UnixNano(), 36)
	areaID := createAreaOverHTTP(t, server.URL, unique)
	t.Cleanup(func() { cleanupAreaLifecycleData(t, pool, areaID) })
	replaceSchedulesOverHTTP(t, server.URL, areaID)
	stationID := createStationOverHTTP(t, server.URL, areaID, unique)
	disableStationOverHTTP(t, server.URL, stationID)
	assertDisabledStationListed(t, server.URL, areaID, stationID)
}

func createAreaOverHTTP(t *testing.T, baseURL, unique string) int64 {
	t.Helper()
	reply := requestJSON(t, http.MethodPost, baseURL+"/v1/admin/service-areas", map[string]any{
		"area_code": "UAT-" + unique, "area_name": "验收运营区域", "city_code": "uat",
		"city_name": "验收城市", "boundary_geojson": "{}", "status": "SERVICE_AREA_STATUS_OPERATING",
	})
	return nestedID(t, reply, "serviceArea")
}

func replaceSchedulesOverHTTP(t *testing.T, baseURL string, areaID int64) {
	t.Helper()
	requestJSON(t, http.MethodPut, baseURL+"/v1/admin/service-areas/"+strconv.FormatInt(areaID, 10)+"/schedules", map[string]any{
		"schedules": []map[string]any{{"day_of_week": 1, "start_minute": 540, "end_minute": 1080, "is_enabled": true}},
	})
}

func createStationOverHTTP(t *testing.T, baseURL string, areaID int64, unique string) int64 {
	t.Helper()
	reply := requestJSON(t, http.MethodPost, baseURL+"/v1/admin/stations", map[string]any{
		"service_area_id": strconv.FormatInt(areaID, 10), "station_code": "UAT-S-" + unique,
		"station_name": "验收站点", "address": "验收路 1 号", "longitude": 116.4, "latitude": 39.9,
		"station_type": 1, "service_direction": 3,
	})
	return nestedID(t, reply, "station")
}

func disableStationOverHTTP(t *testing.T, baseURL string, stationID int64) {
	t.Helper()
	reply := requestJSON(t, http.MethodPatch, baseURL+"/v1/admin/stations/"+strconv.FormatInt(stationID, 10)+"/status", map[string]any{"status": 0})
	station, ok := reply["station"].(map[string]any)
	if !ok || station["status"] != float64(0) {
		t.Fatalf("disabled station reply = %#v, want status 0", reply)
	}
}

func assertDisabledStationListed(t *testing.T, baseURL string, areaID, stationID int64) {
	t.Helper()
	reply := requestJSON(t, http.MethodGet, baseURL+"/v1/admin/stations?service_area_id="+strconv.FormatInt(areaID, 10), nil)
	stations, ok := reply["stations"].([]any)
	if !ok {
		t.Fatalf("station list reply = %#v, want stations", reply)
	}
	for _, value := range stations {
		station, ok := value.(map[string]any)
		if ok && idString(station["id"]) == strconv.FormatInt(stationID, 10) && station["status"] == float64(0) {
			return
		}
	}
	t.Fatalf("station %d is not listed disabled: %#v", stationID, reply)
}

func requestJSON(t *testing.T, method, url string, body any) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status = %d, body = %s", method, url, response.StatusCode, payload)
	}
	decoded := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return decoded
}

func nestedID(t *testing.T, reply map[string]any, key string) int64 {
	t.Helper()
	entity, ok := reply[key].(map[string]any)
	if !ok {
		t.Fatalf("reply = %#v, want %s", reply, key)
	}
	id, err := strconv.ParseInt(idString(entity["id"]), 10, 64)
	if err != nil || id <= 0 {
		t.Fatalf("reply ID = %#v, want positive int64", entity["id"])
	}
	return id
}

func idString(value any) string {
	text, _ := value.(string)
	return text
}

func cleanupAreaLifecycleData(t *testing.T, pool *pgxpool.Pool, areaID int64) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), "DELETE FROM public.stations WHERE service_area_id = $1", areaID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_area_schedules WHERE service_area_id = $1", areaID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM public.service_areas WHERE id = $1", areaID)
}
