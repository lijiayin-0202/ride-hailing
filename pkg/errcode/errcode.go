// Package errcode 收敛跨模块复用的业务错误码（.agent/01 §3：pkg/ 放错误码；07 §3：错误码集中定义）。
//
// 约定：
//  1. 只允许使用接口文档已定义的错误码，不新增未定义枚举（AGENT.MD §5）；
//  2. 各业务层引用本包常量构造 kratos error，禁止在业务代码里写裸字符串；
//  3. 号段规则：本包只放公共码，业务专属码由接口文档定义后追加，禁止自造。
package errcode

// 公共业务错误码（与《萝卜快跑前端页面接口文档》§1.1 公共错误码枚举一致）。
const (
	// InvalidArgument 参数不合法：格式、范围、必填项校验失败。
	InvalidArgument = "INVALID_ARGUMENT"
	// Unauthorized 未登录或令牌失效：缺失、伪造、过期的访问令牌。
	Unauthorized = "UNAUTHORIZED"
	// Forbidden 无权访问：账号冻结/注销等状态拦截。
	Forbidden = "FORBIDDEN"
	// ResourceNotFound 资源不存在：同时用于越权访问，避免探测资源是否存在。
	ResourceNotFound = "RESOURCE_NOT_FOUND"
	// BusinessConflict 业务冲突：重复提交、唯一键占用等可重试场景。
	BusinessConflict = "BUSINESS_CONFLICT"
	// RateLimited 触发限流：验证码发送频率、验证码错误次数超限。
	RateLimited = "RATE_LIMITED"
	// InternalError 内部错误：对外统一返回简化提示，不透传底层细节。
	InternalError = "INTERNAL_ERROR"
)
