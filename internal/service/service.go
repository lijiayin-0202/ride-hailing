package service

import "github.com/google/wire"

// ProviderSet is service providers.
//
// 服务层（空模板，预留位置）：
//   - 每个 proto service 对应一个 XxxService 结构体
//   - 实现 api/ 下生成的 XxxServer 接口（需内嵌 UnimplementedXxxServer）
//   - 只做参数校验与用例编排，不写领域逻辑
//
// 新增服务后在此登记，例如：
//
//	var ProviderSet = wire.NewSet(NewOrderService)
var ProviderSet = wire.NewSet()
