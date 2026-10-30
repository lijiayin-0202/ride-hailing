// 模块1 用户账号与个人中心（成员A）
// 通用枚举、分页归一化与脱敏工具。字段/枚举对齐数据字典与前端接口文档。
package biz

import (
	"strings"
)

// trimSpace 去除首尾空白，业务校验统一入口。
func trimSpace(s string) string { return strings.TrimSpace(s) }

// 账号状态（passengers.account_status）
const (
	AccountStatusFrozen  int16 = 0 // 冻结
	AccountStatusNormal  int16 = 1 // 正常
	AccountStatusDeleted int16 = 2 // 注销
)

// 实名状态（passengers.real_name_status）
const (
	RealNameStatusNone     int16 = 0 // 未认证
	RealNameStatusPending  int16 = 1 // 认证中
	RealNameStatusVerified int16 = 2 // 已认证
	RealNameStatusRejected int16 = 3 // 认证驳回
)

// 列表分页约束：列表接口强制分页，禁止返回全量（AGENT.md §9.3）。
const (
	DefaultPage     int32 = 1
	DefaultPageSize int32 = 20
	MaxPageSize     int32 = 50
)

// NormalizePage 归一化分页参数，返回 page、pageSize、offset。
func NormalizePage(page, pageSize int32) (int32, int32, int32) {
	if page <= 0 {
		page = DefaultPage
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize, (page - 1) * pageSize
}

// 手机号校验与脱敏已下沉到公共包（.agent/01 §3：pkg/ 放脱敏与格式校验）：
//   - 校验入口：pkg/validate.Mobile
//   - 脱敏入口：pkg/mask.Mobile
