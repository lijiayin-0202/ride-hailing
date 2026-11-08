package biz

import (
	"context"
	"fmt"
	"strings"
)

// AdminListOrders 后台订单列表：按状态/关键词筛选并强制分页。
func (uc *RideUsecase) AdminListOrders(ctx context.Context, filter AdminOrderFilter) (*AdminOrderPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return uc.repo.ListOrdersForAdmin(ctx, filter)
}

// AdminListPassengers 后台乘客列表：按关键词/实名状态/账户状态筛选并强制分页。
func (uc *RideUsecase) AdminListPassengers(ctx context.Context, filter AdminPassengerFilter) (*AdminPassengerPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return uc.repo.ListPassengersForAdmin(ctx, filter)
}

// AdminGetPassenger 后台乘客详情。
func (uc *RideUsecase) AdminGetPassenger(ctx context.Context, passengerID int64) (*Passenger, error) {
	if passengerID <= 0 {
		return nil, ErrInvalidArgument
	}
	return uc.repo.GetPassengerAny(ctx, passengerID)
}

// AdminListVehicles 后台车辆列表：按关键词/运营状态/在线状态筛选并强制分页。
func (uc *RideUsecase) AdminListVehicles(ctx context.Context, filter AdminVehicleFilter) (*AdminVehiclePage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return uc.repo.ListVehiclesForAdmin(ctx, filter)
}

// AdminGetVehicle 后台车辆详情。
func (uc *RideUsecase) AdminGetVehicle(ctx context.Context, vehicleID int64) (*AdminVehicle, error) {
	if vehicleID <= 0 {
		return nil, ErrInvalidArgument
	}
	return uc.repo.GetVehicleAny(ctx, vehicleID)
}

// AdminListDispatchRecords 后台派单记录列表：按关键词/状态筛选并强制分页。
func (uc *RideUsecase) AdminListDispatchRecords(ctx context.Context, filter AdminDispatchRecordFilter) (*AdminDispatchRecordPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return uc.repo.ListDispatchRecordsForAdmin(ctx, filter)
}

// AdminGetOrder 后台订单详情：订单本体 + 聚合摘要。
func (uc *RideUsecase) AdminGetOrder(ctx context.Context, orderID int64) (*Order, *OrderLiveView, error) {
	if orderID <= 0 {
		return nil, nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrderAny(ctx, orderID)
	if err != nil {
		return nil, nil, err
	}
	view, err := uc.repo.GetLiveView(ctx, order.PassengerID, orderID)
	if err != nil {
		return nil, nil, err
	}
	return order, view, nil
}

// AdminDispatch 后台派车：0 待派车先派单，再指定（或自动挑选）车辆确认接单。
// 车辆确认会把 trip_orders.vehicle_id 绑定到订单，乘客端即可看到车牌。
func (uc *RideUsecase) AdminDispatch(ctx context.Context, orderID, vehicleID int64) (*Order, *Vehicle, error) {
	if orderID <= 0 {
		return nil, nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrderAny(ctx, orderID)
	if err != nil {
		return nil, nil, err
	}
	switch order.Status {
	case OrderStatusPendingDispatch:
		if _, err := uc.adminCommand(ctx, order, CommandDispatch, 0); err != nil {
			return nil, nil, err
		}
	case OrderStatusDispatching:
		// 已派单，继续确认车辆
	default:
		return nil, nil, fmt.Errorf("%w: order status %d cannot be dispatched", ErrConflict, order.Status)
	}
	vehicle, err := uc.resolveVehicle(ctx, order.ServiceAreaID, vehicleID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := uc.adminCommand(ctx, order, CommandDispatchAccept, vehicle.ID); err != nil {
		return nil, nil, err
	}
	updated, err := uc.repo.GetOrderAny(ctx, orderID)
	if err != nil {
		return nil, nil, err
	}
	return updated, vehicle, nil
}

func (uc *RideUsecase) resolveVehicle(ctx context.Context, serviceAreaID, vehicleID int64) (*Vehicle, error) {
	if vehicleID > 0 {
		vehicle, err := uc.repo.GetVehicle(ctx, vehicleID)
		if err != nil {
			return nil, err
		}
		return vehicle, nil
	}
	vehicle, err := uc.repo.PickAvailableVehicle(ctx, serviceAreaID)
	if err != nil {
		return nil, err
	}
	if vehicle == nil {
		return nil, fmt.Errorf("%w: no available vehicle in service area", ErrConflict)
	}
	return vehicle, nil
}

// AdminCommand 后台推进订单状态（到达/开始/完成/异常关闭）。
func (uc *RideUsecase) AdminCommand(ctx context.Context, orderID int64, commandType string) (*Order, error) {
	if orderID <= 0 || commandType == "" {
		return nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrderAny(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if _, err := uc.adminCommand(ctx, order, strings.ToUpper(commandType), 0); err != nil {
		return nil, err
	}
	return uc.repo.GetOrderAny(ctx, orderID)
}

func (uc *RideUsecase) adminCommand(ctx context.Context, order *Order, commandType string, vehicleID int64) (*OrderCommand, error) {
	command := &OrderCommand{
		OrderID: order.ID, PassengerID: order.PassengerID, Type: commandType, VehicleID: vehicleID,
		IdempotencyKey: fmt.Sprintf("admin-%s-%d-%d", strings.ToLower(commandType), order.ID, uc.now().UnixNano()),
	}
	return uc.ExecuteCommand(ctx, command)
}
