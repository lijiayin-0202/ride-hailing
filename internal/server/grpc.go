// 传输层：gRPC 服务创建与业务服务注册。
// 约定（agent/07 §1）：共享文件只追加自己的注册行，并标注任务号，避免多人并发冲突。
package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

// NewGRPCServer 创建 kratos gRPC 服务并注册业务服务。
func NewGRPCServer(
	c *conf.Server,
	logger log.Logger,
	passengerAccount *service.PassengerAccountService,
) *grpc.Server {
	// 中间件顺序：panic 兜底 → 链路追踪 → 请求日志（不打印参数）。
	opts := []grpc.ServerOption{
		grpc.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			// 与 HTTP 一致：日志只记录操作与耗时，不打印请求参数
			requestLogging(logger),
		),
	}
	// 以下配置项均可省略：未配置时使用 kratos 默认（tcp / :9000 / 无超时）。
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

	// 模块1 用户账号与个人中心（成员A）｜任务号：待登记（agent/07 §1：注册处需标注任务号）
	v1.RegisterPassengerAccountServer(srv, passengerAccount)

	return srv
}
