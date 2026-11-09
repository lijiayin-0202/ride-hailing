package data

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

const dispatchAdminColumns = "d.id, d.dispatch_no, d.order_id, COALESCE(o.order_no, '') AS order_no, " +
	"d.vehicle_id, COALESCE(v.plate_no, '') AS plate_no, COALESCE(d.dispatch_strategy, '') AS dispatch_strategy, " +
	"d.candidate_score::float8 AS candidate_score, d.estimated_pickup_distance_km::float8 AS estimated_pickup_distance_km, " +
	"d.estimated_arrival_min, d.status, COALESCE(d.failure_reason, '') AS failure_reason, " +
	"d.dispatched_at, d.responded_at, d.created_at"

type dispatchAdminRow struct {
	ID                        int64
	DispatchNo                string
	OrderID                   int64
	OrderNo                   string
	VehicleID                 *int64
	PlateNo                   string
	DispatchStrategy          string
	CandidateScore            *float64
	EstimatedPickupDistanceKM *float64
	EstimatedArrivalMin       *int32
	Status                    int16
	FailureReason             string
	DispatchedAt              *time.Time
	RespondedAt               *time.Time
	CreatedAt                 time.Time
}

func (row dispatchAdminRow) toBiz() *biz.AdminDispatchRecord {
	return &biz.AdminDispatchRecord{
		ID: row.ID, DispatchNo: row.DispatchNo, OrderID: row.OrderID, OrderNo: row.OrderNo,
		VehicleID: row.VehicleID, PlateNo: row.PlateNo, DispatchStrategy: row.DispatchStrategy,
		CandidateScore: row.CandidateScore, EstimatedPickupDistanceKM: row.EstimatedPickupDistanceKM,
		EstimatedArrivalMin: row.EstimatedArrivalMin, Status: row.Status, FailureReason: row.FailureReason,
		DispatchedAt: row.DispatchedAt, RespondedAt: row.RespondedAt, CreatedAt: row.CreatedAt,
	}
}

func dispatchAdminQuery(db *gorm.DB) *gorm.DB {
	return db.Table("dispatch_records AS d").
		Joins("LEFT JOIN trip_orders AS o ON o.id = d.order_id").
		Joins("LEFT JOIN vehicles AS v ON v.id = d.vehicle_id")
}

// ListDispatchRecordsForAdmin 后台派单记录分页列表。
func (r *rideRepo) ListDispatchRecordsForAdmin(ctx context.Context, filter biz.AdminDispatchRecordFilter) (*biz.AdminDispatchRecordPage, error) {
	var total int64
	if err := applyAdminDispatchFilter(dispatchAdminQuery(r.data.gormDB.WithContext(ctx)), filter).Count(&total).Error; err != nil {
		return nil, err
	}
	page := &biz.AdminDispatchRecordPage{Total: total}
	if total == 0 {
		return page, nil
	}
	var rows []dispatchAdminRow
	offset := (filter.Page - 1) * filter.PageSize
	query := applyAdminDispatchFilter(dispatchAdminQuery(r.data.gormDB.WithContext(ctx)), filter).Select(dispatchAdminColumns)
	if err := query.Order("d.created_at DESC, d.id DESC").Limit(filter.PageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		page.Items = append(page.Items, rows[index].toBiz())
	}
	return page, nil
}

func applyAdminDispatchFilter(query *gorm.DB, filter biz.AdminDispatchRecordFilter) *gorm.DB {
	if filter.Status != nil {
		query = query.Where("d.status = ?", *filter.Status)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		if orderID, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			query = query.Where("(d.dispatch_no ILIKE ? OR o.order_no ILIKE ? OR v.plate_no ILIKE ? OR d.order_id = ?)", like, like, like, orderID)
		} else {
			query = query.Where("(d.dispatch_no ILIKE ? OR o.order_no ILIKE ? OR v.plate_no ILIKE ?)", like, like, like)
		}
	}
	return query
}
