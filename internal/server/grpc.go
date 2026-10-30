package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

// NewGRPCServer 创建 gRPC 边界；车辆端通过该服务回传确认或拒绝。
func NewGRPCServer(c *conf.Server, logger log.Logger, stationService *service.StationService, serviceAreaAdminService *service.ServiceAreaAdminService, callbackService *service.DispatchCallbackService) *grpc.Server {
	opts := []grpc.ServerOption{
		grpc.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			logging.Server(logger),
		),
	}
	if c.GetGrpc().GetNetwork() != "" {
		opts = append(opts, grpc.Network(c.GetGrpc().GetNetwork()))
	}
	if c.GetGrpc().GetAddr() != "" {
		opts = append(opts, grpc.Address(c.GetGrpc().GetAddr()))
	}
	if c.GetGrpc().GetTimeout() != nil {
		opts = append(opts, grpc.Timeout(c.GetGrpc().GetTimeout().AsDuration()))
	}

	srv := grpc.NewServer(opts...)
	v1.RegisterStationServiceServer(srv, stationService)
	v1.RegisterServiceAreaAdminServiceServer(srv, serviceAreaAdminService)
	// 回调与 HTTP 派单入口分离，便于模块 4 在内网可靠上报车辆事实。
	v1.RegisterDispatchCallbackServiceServer(srv, callbackService)

	return srv
}
