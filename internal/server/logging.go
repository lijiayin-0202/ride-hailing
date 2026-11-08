// 模块1 用户账号与个人中心（成员A）
// 请求日志中间件：只记录 kind/operation/业务码/耗时，不记录请求参数，
// 避免手机号、验证码、地址等隐私数据进入日志（AGENT.md §10.2）。
//
// 说明：kratos 自带 logging.Server 会把请求参数整体打印，本模块涉及手机号登录，
// 因此替换为本实现；公共日志规范由模块6统一后再合并。
package server

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"

	kerrors "github.com/go-kratos/kratos/v2/errors"
)

// requestLogging 请求日志中间件：包住 handler，记录调用结果与耗时。
// 刻意不打印 req 内容，防止敏感字段泄进日志。
func requestLogging(logger log.Logger) middleware.Middleware {
	helper := log.NewHelper(log.With(logger, "module", "server/request_logging"))
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			// 从传输层上下文取协议类型与操作名（如 HTTP / POST /v1/xxx）。
			var kind, operation string
			if info, ok := transport.FromServerContext(ctx); ok {
				kind = info.Kind().String()
				operation = info.Operation()
			}

			// 计时起点放在 handler 调用前。
			start := time.Now()
			reply, err := handler(ctx, req)

			// 默认按成功记录，出错时取 kratos error 的 HTTP 状态与业务码。
			code := 200
			reason := ""
			if err != nil {
				se := kerrors.FromError(err)
				code = int(se.Code)
				reason = se.Reason
			}
			// 只输出维度信息与耗时，不输出请求参数。
			helper.WithContext(ctx).Infof("kind=%s operation=%s code=%d reason=%s latency=%s",
				kind, operation, code, reason, time.Since(start))
			return reply, err
		}
	}
}
