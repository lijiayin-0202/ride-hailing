package biz

import "github.com/google/wire"

// ProviderSet 集中登记各业务模块的领域用例。
var ProviderSet = wire.NewSet(
	// 模块1 用户账号与个人中心。
	NewAuthUsecase,
	NewPassengerUsecase,
	NewAddressUsecase,
	NewCouponUsecase,
	NewPointUsecase,
	NewNotificationUsecase,
	ProvideRealNameSalt,
	ProvideRealNameBox,
	NewRealNameUsecase,
	NewLoginDeviceUsecase,
	NewEmergencyContactUsecase,
	// 模块3 运营区域、站点与派单。
	NewDispatchUsecase,
	NewDispatchStateUsecase,
	NewAutoDispatchUsecase,
	NewSystemClock,
	NewServiceAreaAvailabilityUsecase,
	NewServiceAreaAdminUsecase,
	NewStationUsecase,
	NewVehicleUsecase,
)
