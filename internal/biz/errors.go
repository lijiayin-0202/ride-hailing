// 模块1 用户账号与个人中心（成员A）
// 错误码严格使用《萝卜快跑前端页面接口文档》§1.1 公共错误码枚举，不新增未定义错误码。
package biz

import (
	"errors"
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

	// 密码登录（UA-T0-05）
	// ErrPasswordNotSet 手机号注册的账号未设置密码，提示改用验证码登录。
	ErrPasswordNotSet = kerrors.BadRequest(CodeInvalidArgument, "该账号未设置密码，请使用验证码登录")
	// ErrPasswordIncorrect 账号不存在与密码错误统一话术，避免手机号被枚举。
	ErrPasswordIncorrect = kerrors.Unauthorized(CodeUnauthorized, "手机号或密码不正确")
	// ErrPasswordAttemptLimited 连续密码错误次数超限，短时间锁定（复用限流码，不新增错误码）。
	ErrPasswordAttemptLimited = kerrors.New(http.StatusTooManyRequests, CodeRateLimited, "密码错误次数过多，请15分钟后再试")
)

// invalidArgument 在通用错误上覆盖具体提示，便于前端直接展示。
func invalidArgument(format string, args ...any) error {
	e := kerrors.Clone(ErrInvalidArgument)
	e.Message = fmt.Sprintf(format, args...)
	return e
}

// BusinessError 是车辆 HMI 层使用的统一错误描述，复用当前项目既有错误码。
type BusinessError struct {
	Status  int
	Reason  string
	Message string
}

// ToBusinessError 将 HMI 领域错误映射为对外可识别的业务错误。
func ToBusinessError(err error) BusinessError {
	switch {
	case errors.Is(err, ErrInvalidVehicleInput):
		return BusinessError{Status: http.StatusBadRequest, Reason: CodeInvalidArgument, Message: "车辆端请求参数不合法"}
	case errors.Is(err, ErrInvalidCommandTransition):
		return BusinessError{Status: http.StatusConflict, Reason: CodeBusinessConflict, Message: "车辆指令状态流转冲突"}
	default:
		return BusinessError{Status: http.StatusInternalServerError, Reason: CodeInternalError, Message: "车辆端服务内部错误"}
	}
}

// ToTransportError 将 HMI 业务错误转换为 Kratos HTTP/gRPC 标准错误。
func ToTransportError(item BusinessError) error {
	return kerrors.New(item.Status, item.Reason, item.Message)
}

// tooManyRequests 生成带具体提示的 429 错误：复用既有限流码，不新增未定义错误码。
func tooManyRequests(message string) error {
	e := kerrors.Clone(ErrSmsTooFrequent)
	e.Message = message
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
