package data

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

const vehicleAdminColumns = "v.id, v.vehicle_no, v.vin, v.plate_no, v.vehicle_model, v.seat_count, v.service_area_id, " +
	"COALESCE(a.area_name, '') AS service_area_name, v.operation_status, v.online_status, " +
	"v.battery_percent::float8 AS battery_percent, v.software_version, v.last_reported_at, v.created_at"

type vehicleAdminRow struct {
	ID              int64
	VehicleNo       string
	VIN             string
	PlateNo         string
	VehicleModel    string
	SeatCount       int16
	ServiceAreaID   *int64
	ServiceAreaName string
	OperationStatus int16
	OnlineStatus    int16
	BatteryPercent  *float64
	SoftwareVersion *string
	LastReportedAt  *time.Time
	CreatedAt       time.Time
}

func (row vehicleAdminRow) toBiz() *biz.AdminVehicle {
	vehicle := &biz.AdminVehicle{
		ID: row.ID, VehicleNo: row.VehicleNo, VIN: row.VIN, PlateNo: row.PlateNo, Model: row.VehicleModel,
		SeatCount: row.SeatCount, ServiceAreaID: row.ServiceAreaID, ServiceAreaName: row.ServiceAreaName,
		OperationStatus: row.OperationStatus, OnlineStatus: row.OnlineStatus, BatteryPercent: row.BatteryPercent,
		LastReportedAt: row.LastReportedAt, CreatedAt: row.CreatedAt,
	}
	if row.SoftwareVersion != nil {
		vehicle.SoftwareVersion = *row.SoftwareVersion
	}
	return vehicle
}

func vehicleAdminQuery(db *gorm.DB) *gorm.DB {
	return db.Table("vehicles AS v").Joins("LEFT JOIN service_areas AS a ON a.id = v.service_area_id")
}

// ListVehiclesForAdmin 后台车辆分页列表。
func (r *rideRepo) ListVehiclesForAdmin(ctx context.Context, filter biz.AdminVehicleFilter) (*biz.AdminVehiclePage, error) {
	var total int64
	if err := applyAdminVehicleFilter(vehicleAdminQuery(r.data.gormDB.WithContext(ctx)), filter).Count(&total).Error; err != nil {
		return nil, err
	}
	page := &biz.AdminVehiclePage{Total: total}
	if total == 0 {
		return page, nil
	}
	var rows []vehicleAdminRow
	offset := (filter.Page - 1) * filter.PageSize
	query := applyAdminVehicleFilter(vehicleAdminQuery(r.data.gormDB.WithContext(ctx)), filter).Select(vehicleAdminColumns)
	if err := query.Order("v.created_at DESC, v.id DESC").Limit(filter.PageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		page.Items = append(page.Items, rows[index].toBiz())
	}
	return page, nil
}

// GetVehicleAny 后台车辆详情，不校验归属。
func (r *rideRepo) GetVehicleAny(ctx context.Context, vehicleID int64) (*biz.AdminVehicle, error) {
	var row vehicleAdminRow
	err := vehicleAdminQuery(r.data.gormDB.WithContext(ctx)).Select(vehicleAdminColumns).
		Where("v.id = ?", vehicleID).Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound || isOptionalRelationError(err) {
			return nil, biz.ErrNotFound
		}
		return nil, err
	}
	return row.toBiz(), nil
}

func applyAdminVehicleFilter(query *gorm.DB, filter biz.AdminVehicleFilter) *gorm.DB {
	if filter.OperationStatus != nil {
		query = query.Where("v.operation_status = ?", *filter.OperationStatus)
	}
	if filter.OnlineStatus != nil {
		query = query.Where("v.online_status = ?", *filter.OnlineStatus)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		if vehicleID, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			query = query.Where("(v.plate_no ILIKE ? OR v.vehicle_no ILIKE ? OR v.vin ILIKE ? OR v.vehicle_model ILIKE ? OR v.id = ?)", like, like, like, like, vehicleID)
		} else {
			query = query.Where("(v.plate_no ILIKE ? OR v.vehicle_no ILIKE ? OR v.vin ILIKE ? OR v.vehicle_model ILIKE ?)", like, like, like, like)
		}
	}
	return query
}
