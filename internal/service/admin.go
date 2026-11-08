package service

import (
	"strconv"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// 后台管理接口：仅供 GVA 后台调用，统一由 X-Internal-Token 保护。

type adminOrderItem struct {
	OrderID         string  `json:"orderId"`
	OrderNo         string  `json:"orderNo"`
	PassengerID     int64   `json:"passengerId"`
	Status          string  `json:"status"`
	StatusValue     int16   `json:"statusValue"`
	StatusText      string  `json:"statusText"`
	OriginName      string  `json:"originName"`
	DestinationName string  `json:"destinationName"`
	PayableAmount   float64 `json:"payableAmount"`
	VehiclePlate    string  `json:"vehiclePlate"`
	CreatedAt       string  `json:"createdAt"`
}

type adminOrderDetail struct {
	adminOrderItem
	VehicleModel string          `json:"vehicleModel"`
	ServiceType  string          `json:"serviceType"`
	Actions      []string        `json:"actions"`
	Location     any             `json:"location"`
	Dispatch     any             `json:"dispatch"`
	Boarding     any             `json:"boarding"`
	TripProgress *tripProgressJS `json:"tripProgress"`
}

type tripProgressJS struct {
	RemainingDistanceMeters  int32   `json:"remainingDistanceMeters"`
	RemainingDurationSeconds int32   `json:"remainingDurationSeconds"`
	SpeedKMH                 float64 `json:"speedKmh"`
	ProgressPercent          int32   `json:"progressPercent"`
}

// AdminListOrders 后台订单列表。
func (s *RideService) AdminListOrders(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	page, _ := strconv.Atoi(ctx.Query().Get("page"))
	pageSize, _ := strconv.Atoi(ctx.Query().Get("pageSize"))
	filter := biz.AdminOrderFilter{Page: page, PageSize: pageSize, Keyword: ctx.Query().Get("keyword")}
	if statusValue := ctx.Query().Get("status"); statusValue != "" {
		parsed, err := strconv.Atoi(statusValue)
		if err != nil || parsed < 0 || parsed > 7 {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid status")
		}
		status := int16(parsed)
		filter.Status = &status
	}
	result, err := s.uc.AdminListOrders(ctx, filter)
	if err != nil {
		return serviceError(err)
	}
	items := make([]adminOrderItem, 0, len(result.Items))
	for _, order := range result.Items {
		items = append(items, adminItemFromOrder(order))
	}
	return ctx.Result(200, map[string]any{"items": items, "total": result.Total, "page": filter.Page, "pageSize": filter.PageSize})
}

// AdminGetOrder 后台订单详情。
func (s *RideService) AdminGetOrder(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	orderID, err := parseID(ctx.Vars().Get("order_id"))
	if err != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid orderId")
	}
	order, view, usecaseErr := s.uc.AdminGetOrder(ctx, orderID)
	if usecaseErr != nil {
		return serviceError(usecaseErr)
	}
	_, _, _, actions := biz.StatusPresentation(order.Status)
	progress := biz.BuildTripProgress(order, view.Location)
	detail := adminOrderDetail{
		adminOrderItem: adminItemFromOrder(order),
		ServiceType:    order.ServiceType.DisplayName,
		Actions:        actions,
		TripProgress: &tripProgressJS{
			RemainingDistanceMeters: progress.RemainingDistanceMeters, RemainingDurationSeconds: progress.RemainingDurationSeconds,
			SpeedKMH: progress.SpeedKMH, ProgressPercent: progress.ProgressPercent,
		},
	}
	if order.Vehicle != nil {
		detail.VehicleModel = order.Vehicle.Model
	}
	if view.Location != nil {
		detail.Location = map[string]any{
			"longitude": view.Location.Longitude, "latitude": view.Location.Latitude,
			"speedKmh": view.Location.SpeedKMH, "reportedAt": formatTime(&view.Location.ReportedAt),
		}
	}
	if view.Dispatch != nil {
		detail.Dispatch = map[string]any{
			"strategy": view.Dispatch.Strategy, "status": view.Dispatch.Status,
			"attemptCount": view.Dispatch.AttemptCount, "pickupEtaSeconds": view.Dispatch.PickupETASeconds,
			"dispatchedAt": formatTime(view.Dispatch.DispatchedAt),
		}
	}
	if view.Boarding != nil {
		detail.Boarding = map[string]any{
			"method": view.Boarding.Method, "status": view.Boarding.Status, "verifiedAt": formatTime(view.Boarding.VerifiedAt),
		}
	}
	return ctx.Result(200, map[string]any{"order": detail, "safetyEvents": len(view.Safety)})
}

type adminDispatchRequest struct {
	VehicleID int64 `json:"vehicleId"`
}

// AdminDispatch 后台派车：一次完成派单 + 车辆确认。
func (s *RideService) AdminDispatch(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	orderID, err := parseID(ctx.Vars().Get("order_id"))
	if err != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid orderId")
	}
	var request adminDispatchRequest
	if bindErr := ctx.Bind(&request); bindErr != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	order, vehicle, usecaseErr := s.uc.AdminDispatch(ctx, orderID, request.VehicleID)
	if usecaseErr != nil {
		return serviceError(usecaseErr)
	}
	item := adminItemFromOrder(order)
	item.VehiclePlate = vehicle.PlateNo
	return ctx.Result(200, map[string]any{"order": item, "vehicle": map[string]any{"vehicleId": vehicle.ID, "plateNo": vehicle.PlateNo, "model": vehicle.Model}})
}

type adminCommandRequest struct {
	CommandType string `json:"commandType"`
}

// AdminCommand 后台推进订单状态（到达/开始/完成/异常关闭）。
func (s *RideService) AdminCommand(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	orderID, err := parseID(ctx.Vars().Get("order_id"))
	if err != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid orderId")
	}
	var request adminCommandRequest
	if bindErr := ctx.Bind(&request); bindErr != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	order, usecaseErr := s.uc.AdminCommand(ctx, orderID, request.CommandType)
	if usecaseErr != nil {
		return serviceError(usecaseErr)
	}
	return ctx.Result(200, map[string]any{"order": adminItemFromOrder(order)})
}

func adminItemFromOrder(order *biz.Order) adminOrderItem {
	status, text, _, _ := biz.StatusPresentation(order.Status)
	item := adminOrderItem{
		OrderID: formatID("ord", order.ID), OrderNo: order.OrderNo, PassengerID: order.PassengerID,
		Status: status, StatusValue: int16(order.Status), StatusText: text,
		OriginName: order.Origin.Name, DestinationName: order.Destination.Name,
		PayableAmount: toYuan(order.Fare.PayableCents),
		CreatedAt:     order.RequestedAt.Format(time.RFC3339),
	}
	if order.Vehicle != nil {
		item.VehiclePlate = order.Vehicle.PlateNo
	}
	return item
}
