package service

import "github.com/google/wire"

// ProviderSet is service providers.
//
// 服务层：每个 proto service 对应一个 XxxService 结构体，
// 只做参数校验与用例编排，不写领域逻辑、不访问数据库。
var ProviderSet = wire.NewSet(
	// 模块1 用户账号与个人中心（成员A）
	NewPassengerAccountService,
)
