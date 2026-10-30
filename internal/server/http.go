// 传输层：HTTP 服务创建、路由注册与统一响应/错误编码。
// 约定（agent/07 §1）：各成员只追加自己的注册行并标注任务号，避免共享文件互相覆盖。
package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

// NewHTTPServer 创建 kratos HTTP 服务并注册业务路由。
//
// 各成员新增服务时，在参数尾部追加自己的 service 并在下方注册路由，
// 避免共享服务器文件互相覆盖。
func NewHTTPServer(
	c *conf.Server,
	logger log.Logger,
	passengerAccount *service.PassengerAccountService,
) *http.Server {
	// 中间件顺序：panic 兜底 → 链路追踪 → 请求日志；并挂统一响应/错误编码器。
	opts := []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			// 不使用 kratos 默认 logging.Server：它会把请求参数（手机号等）打进日志
			requestLogging(logger),
		),
		// 统一返回体 { code, message, requestId, data }（前端页面接口文档 §1.1）
		http.ResponseEncoder(ResponseEncoder),
		http.ErrorEncoder(ErrorEncoder),
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

	// 模块1 用户账号与个人中心（成员A）｜任务号：待登记（agent/07 §1：注册处需标注任务号）
	v1.RegisterPassengerAccountHTTPServer(srv, passengerAccount)

	return srv
}
