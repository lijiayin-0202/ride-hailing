package biz

import "github.com/google/wire"

// ProviderSet is biz providers.
//
// 业务用例层：领域实体、用例（Usecase）放在本目录；
// 依赖通过接口（Repo）注入，接口定义在本层，实现放在 internal/data。
var ProviderSet = wire.NewSet(
	// 模块1 用户账号与个人中心（成员A）
	NewAuthUsecase,
	NewPassengerUsecase,
	NewAddressUsecase,
	NewCouponUsecase,
	NewPointUsecase,
	NewNotificationUsecase,
	// 实名认证：盐值由 conf 提供（独立类型，避免 wire 混淆 string 依赖）
	ProvideRealNameSalt,
	NewRealNameUsecase,
	// 登录设备与登录记录（账号管理页）
	NewLoginDeviceUsecase,
)
