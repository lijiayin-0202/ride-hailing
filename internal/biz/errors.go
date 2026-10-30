// 模块1 用户账号与个人中心（成员A）
// 错误码严格使用《萝卜快跑前端页面接口文档》§1.1 公共错误码枚举，不新增未定义错误码。
package biz

import (
	"fmt"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v2/errors"

	"github.com/lijayin-0202/radish-ride-service/pkg/errcode"
)

// 公共错误码统一来自 pkg/errcode（.agent/01 §3、07 §3：错误码集中定义，跨模块复用同一号段）。
// 这里只做别名导出，保持本模块既有引用路径不变。
const (
	CodeInvalidArgument  = errcode.InvalidArgument
	CodeUnauthorized     = errcode.Unauthorized
	CodeForbidden        = errcode.Forbidden
	CodeResourceNotFound = errcode.ResourceNotFound
	CodeBusinessConflict = errcode.BusinessConflict
	CodeRateLimited      = errcode.RateLimited
	CodeInternalError    = errcode.InternalError
)

// 模块1 领域错误。消息面向用户，不包含内部实现细节。
var (
	ErrInvalidArgument   = kerrors.BadRequest(CodeInvalidArgument, "参数不合法")
	ErrUnauthorized      = kerrors.Unauthorized(CodeUnauthorized, "未登录或令牌失效")
	ErrForbidden         = kerrors.Forbidden(CodeForbidden, "无权访问该资源")
	ErrPassengerNotFound = kerrors.NotFound(CodeResourceNotFound, "乘客不存在")
	ErrAddressNotFound   = kerrors.NotFound(CodeResourceNotFound, "常用地址不存在")

	// ErrAccountDisabled 账号冻结/注销（passengers.account_status != 1）
	ErrAccountDisabled = kerrors.Forbidden(CodeForbidden, "账号已被冻结或注销，请联系客服")

	// 登录验证码相关
	ErrSmsCodeExpired   = kerrors.BadRequest(CodeInvalidArgument, "验证码已失效，请重新获取")
	ErrSmsCodeIncorrect = kerrors.BadRequest(CodeInvalidArgument, "验证码不正确")
	// 429 限流错误：kratos errors 未提供 TooManyRequests 快捷方法，使用 New 构造。
	ErrSmsTooFrequent    = kerrors.New(http.StatusTooManyRequests, CodeRateLimited, "验证码发送过于频繁，请稍后再试")
	ErrSmsAttemptLimited = kerrors.New(http.StatusTooManyRequests, CodeRateLimited, "验证码错误次数过多，请重新获取")

	// 地址默认项冲突等可重试的业务冲突
	ErrBusinessConflict = kerrors.Conflict(CodeBusinessConflict, "操作冲突，请刷新后重试")
	ErrInternal         = kerrors.InternalServer(CodeInternalError, "服务内部错误")

	// 实名认证提交频控：复用限流码，不新增未定义错误码。
	ErrRealNameTooFrequent = kerrors.New(http.StatusTooManyRequests, CodeRateLimited, "提交过于频繁，请稍后再试")
)

// invalidArgument 在通用错误上覆盖具体提示，便于前端直接展示。
func invalidArgument(format string, args ...any) error {
	e := kerrors.Clone(ErrInvalidArgument)
	e.Message = fmt.Sprintf(format, args...)
	return e
}

// businessConflict 在通用冲突错误上覆盖具体提示（如实名认证的重复提交/证件重绑）。
func businessConflict(format string, args ...any) error {
	e := kerrors.Clone(ErrBusinessConflict)
	e.Message = fmt.Sprintf(format, args...)
	return e
}

// wrapInternal 归一化未知异常，避免把底层错误透传给客户端。
func wrapInternal(err error) error {
	e := kerrors.Clone(ErrInternal)
	e.Metadata = map[string]string{"cause": err.Error()}
	return e
}
