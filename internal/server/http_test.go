package server

import (
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	kratosHTTP "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

func TestNewHTTPServerRegistersVehicleHMIRoutes(t *testing.T) {
	svc := service.NewVehicleHMIService(biz.NewVehicleHMIUsecase(nil, log.DefaultLogger), log.DefaultLogger)
	server := NewHTTPServer(&conf.Server{}, log.DefaultLogger, nil, nil, nil, nil, nil, svc)
	routes := make([]kratosHTTP.RouteInfo, 0)
	err := server.WalkRoute(func(route kratosHTTP.RouteInfo) error {
		routes = append(routes, route)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkRoute() error = %v", err)
	}
	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{
		"GET /api/v1/vehicle/commands/pending",
		"POST /api/v1/vehicle/telemetry",
		"POST /api/v1/vehicle/faults",
		"GET /api/v1/vehicle/orders/{order_id}/hmi-view",
	} {
		if !registered[path] {
			t.Fatalf("missing HMI route %s", path)
		}
	}
}
