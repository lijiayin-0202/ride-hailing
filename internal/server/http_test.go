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
	server := NewHTTPServer(&conf.Server{}, log.DefaultLogger, svc)
	routes := make([]kratosHTTP.RouteInfo, 0, 9)
	err := server.WalkRoute(func(route kratosHTTP.RouteInfo) error {
		routes = append(routes, route)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkRoute() error = %v", err)
	}
	if len(routes) != 9 {
		t.Fatalf("registered routes = %d, want 9", len(routes))
	}
}
