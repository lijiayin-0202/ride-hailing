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

// NewHTTPServer 创建 HTTP 边界；派单服务在此暴露订单 ID 驱动的自动派单接口。
func NewHTTPServer(c *conf.Server, logger log.Logger, stationService *service.StationService, serviceAreaAdminService *service.ServiceAreaAdminService, dispatchService *service.DispatchService) *http.Server {
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
	v1.RegisterStationServiceHTTPServer(srv, stationService)
	v1.RegisterServiceAreaAdminServiceHTTPServer(srv, serviceAreaAdminService)
	// 注解生成的路由映射在 proto 中维护，避免手写路径与契约漂移。
	v1.RegisterDispatchServiceHTTPServer(srv, dispatchService)

	return srv
}
