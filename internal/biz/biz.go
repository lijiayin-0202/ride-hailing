package biz

import "github.com/google/wire"

// ProviderSet is biz providers.
//
// 业务用例层（空模板，预留位置）：
//   - 领域实体、领域服务、Usecase 均放在本目录
//   - 依赖通过接口（Repo）注入，接口定义放在本层，实现放在 internal/data
//
// 新增用例后在此登记，例如：
//
//	var ProviderSet = wire.NewSet(NewOrderUsecase)
var ProviderSet = wire.NewSet()
