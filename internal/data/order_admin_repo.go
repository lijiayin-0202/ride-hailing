package data

import (
	"context"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// GetOrderAny 不校验乘客归属，仅供后台内部接口使用。
func (r *rideRepo) GetOrderAny(ctx context.Context, orderID int64) (*biz.Order, error) {
	row, err := r.loadOrder(ctx, "o.id = ?", orderID)
	if err != nil {
		return nil, err
	}
	return orderFromDetail(row), nil
}

// ListOrdersForAdmin 后台订单分页列表，复用乘客端详情查询保证字段口径一致。
func (r *rideRepo) ListOrdersForAdmin(ctx context.Context, filter biz.AdminOrderFilter) (*biz.AdminOrderPage, error) {
	countQuery := applyAdminOrderFilter(r.data.gormDB.WithContext(ctx).Table("trip_orders AS o"), filter)
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}
	page := &biz.AdminOrderPage{Total: total}
	if total == 0 {
		return page, nil
	}
	var rows []orderDetailRow
	offset := (filter.Page - 1) * filter.PageSize
	query := applyAdminOrderFilter(r.orderDetailsQuery(ctx), filter)
	if err := query.Order("o.created_at DESC").Limit(filter.PageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		page.Items = append(page.Items, orderFromDetail(rows[index]))
	}
	return page, nil
}

func applyAdminOrderFilter(query *gorm.DB, filter biz.AdminOrderFilter) *gorm.DB {
	if filter.Status != nil {
		query = query.Where("o.status = ?", *filter.Status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		if passengerID, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			query = query.Where("(o.order_no ILIKE ? OR o.passenger_id = ?)", like, passengerID)
		} else {
			query = query.Where("(o.order_no ILIKE ? OR o.passenger_id::text = ?)", like, keyword)
		}
	}
	return query
}

func (r *rideRepo) GetVehicle(ctx context.Context, vehicleID int64) (*biz.RideVehicle, error) {
	var row vehicleRow
	err := r.data.gormDB.WithContext(ctx).Table("vehicles").
		Select("id, vehicle_no, plate_no, vehicle_model").
		Where("id = ?", vehicleID).Take(&row).Error
	if err != nil {
		if isOptionalRelationError(err) {
			return nil, biz.ErrNotFound
		}
		return nil, err
	}
	return &biz.RideVehicle{ID: row.ID, PlateNo: row.PlateNo, Model: row.VehicleModel, BodyNo: row.VehicleNo}, nil
}

// PickAvailableVehicle 挑选区域内在线、运营中且未被活动订单占用的车辆，电量高者优先。
func (r *rideRepo) PickAvailableVehicle(ctx context.Context, serviceAreaID int64) (*biz.RideVehicle, error) {
	var row vehicleRow
	err := r.data.gormDB.WithContext(ctx).Raw(`
		SELECT v.id, v.vehicle_no, v.plate_no, v.vehicle_model
		FROM vehicles v
		WHERE v.service_area_id = ? AND v.online_status = 1 AND v.operation_status = 1
		  AND NOT EXISTS (
			SELECT 1 FROM trip_orders o
			WHERE o.vehicle_id = v.id AND o.status BETWEEN 0 AND 4
		  )
		ORDER BY v.battery_percent DESC NULLS LAST, v.id
		LIMIT 1`, serviceAreaID).Scan(&row).Error
	if err != nil {
		if isOptionalRelationError(err) {
			return nil, nil
		}
		return nil, err
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &biz.RideVehicle{ID: row.ID, PlateNo: row.PlateNo, Model: row.VehicleModel, BodyNo: row.VehicleNo}, nil
}

type vehicleRow struct {
	ID           int64
	VehicleNo    string
	PlateNo      string
	VehicleModel string
}
