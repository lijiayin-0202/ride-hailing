package service

import (
	"strconv"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type adminVehicleItem struct {
	VehicleID       int64    `json:"vehicleId"`
	VehicleNo       string   `json:"vehicleNo"`
	VIN             string   `json:"vin"`
	PlateNo         string   `json:"plateNo"`
	Model           string   `json:"model"`
	SeatCount       int16    `json:"seatCount"`
	ServiceAreaID   *int64   `json:"serviceAreaId"`
	ServiceAreaName string   `json:"serviceAreaName"`
	OperationStatus int16    `json:"operationStatus"`
	OnlineStatus    int16    `json:"onlineStatus"`
	BatteryPercent  *float64 `json:"batteryPercent"`
	SoftwareVersion string   `json:"softwareVersion"`
	LastReportedAt  string   `json:"lastReportedAt"`
	CreatedAt       string   `json:"createdAt"`
}

// AdminListVehicles 后台车辆列表。
func (s *RideService) AdminListVehicles(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	page, _ := strconv.Atoi(ctx.Query().Get("page"))
	pageSize, _ := strconv.Atoi(ctx.Query().Get("pageSize"))
	filter := biz.AdminVehicleFilter{Page: page, PageSize: pageSize, Keyword: ctx.Query().Get("keyword")}
	if value := ctx.Query().Get("operationStatus"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 5 {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid operationStatus")
		}
		status := int16(parsed)
		filter.OperationStatus = &status
	}
	if value := ctx.Query().Get("onlineStatus"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 1 {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid onlineStatus")
		}
		status := int16(parsed)
		filter.OnlineStatus = &status
	}
	result, err := s.uc.AdminListVehicles(ctx, filter)
	if err != nil {
		return serviceError(err)
	}
	items := make([]adminVehicleItem, 0, len(result.Items))
	for _, vehicle := range result.Items {
		items = append(items, adminItemFromVehicle(vehicle))
	}
	return ctx.Result(200, map[string]any{"items": items, "total": result.Total, "page": filter.Page, "pageSize": filter.PageSize})
}

// AdminGetVehicle 后台车辆详情。
func (s *RideService) AdminGetVehicle(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	vehicleID, err := parseID(ctx.Vars().Get("vehicle_id"))
	if err != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid vehicleId")
	}
	vehicle, usecaseErr := s.uc.AdminGetVehicle(ctx, vehicleID)
	if usecaseErr != nil {
		return serviceError(usecaseErr)
	}
	return ctx.Result(200, map[string]any{"vehicle": adminItemFromVehicle(vehicle)})
}

func adminItemFromVehicle(vehicle *biz.AdminVehicle) adminVehicleItem {
	return adminVehicleItem{
		VehicleID: vehicle.ID, VehicleNo: vehicle.VehicleNo, VIN: vehicle.VIN, PlateNo: vehicle.PlateNo,
		Model: vehicle.Model, SeatCount: vehicle.SeatCount, ServiceAreaID: vehicle.ServiceAreaID,
		ServiceAreaName: vehicle.ServiceAreaName, OperationStatus: vehicle.OperationStatus,
		OnlineStatus: vehicle.OnlineStatus, BatteryPercent: vehicle.BatteryPercent,
		SoftwareVersion: vehicle.SoftwareVersion, LastReportedAt: formatOptionalTime(vehicle.LastReportedAt),
		CreatedAt: vehicle.CreatedAt.Format(time.RFC3339),
	}
}
