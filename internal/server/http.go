package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

func NewHTTPServer(c *conf.Server, logger log.Logger, rideService *service.RideService) *http.Server {
	opts := []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			logging.Server(logger),
		),
	}
	if c.GetHttp().GetNetwork() != "" {
		opts = append(opts, http.Network(c.GetHttp().GetNetwork()))
	}
	if c.GetHttp().GetAddr() != "" {
		opts = append(opts, http.Address(c.GetHttp().GetAddr()))
	}
	if c.GetHttp().GetTimeout() != nil {
		opts = append(opts, http.Timeout(c.GetHttp().GetTimeout().AsDuration()))
	}

	srv := http.NewServer(opts...)
	v1.RegisterRideServiceHTTPServer(srv, rideService)
	srv.Route("/").GET("/api/v1/orders/{order_id}/events", rideService.StreamOrderEventsHTTP)

	return srv
}
