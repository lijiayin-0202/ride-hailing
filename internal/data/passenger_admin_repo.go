package data

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

const passengerAdminColumns = "id, passenger_no, mobile, nickname, avatar_url, external_provider, real_name_status, account_status, point_balance, last_login_at, created_at"

type passengerRow struct {
	ID               int64
	PassengerNo      string
	Mobile           string
	Nickname         string
	AvatarURL        *string
	ExternalProvider *string
	RealNameStatus   int16
	AccountStatus    int16
	PointBalance     int32
	LastLoginAt      *time.Time
	CreatedAt        time.Time
}

func (row passengerRow) toBiz() *biz.AdminPassenger {
	passenger := &biz.AdminPassenger{
		ID: row.ID, PassengerNo: row.PassengerNo, Mobile: row.Mobile, Nickname: row.Nickname,
		RealNameStatus: row.RealNameStatus, AccountStatus: row.AccountStatus,
		PointBalance: row.PointBalance, LastLoginAt: row.LastLoginAt, CreatedAt: row.CreatedAt,
	}
	if row.AvatarURL != nil {
		passenger.AvatarURL = *row.AvatarURL
	}
	if row.ExternalProvider != nil {
		passenger.ExternalProvider = *row.ExternalProvider
	}
	return passenger
}

// ListPassengersForAdmin 后台乘客分页列表。
func (r *rideRepo) ListPassengersForAdmin(ctx context.Context, filter biz.AdminPassengerFilter) (*biz.AdminPassengerPage, error) {
	var total int64
	if err := applyAdminPassengerFilter(r.data.gormDB.WithContext(ctx).Table("passengers"), filter).Count(&total).Error; err != nil {
		return nil, err
	}
	page := &biz.AdminPassengerPage{Total: total}
	if total == 0 {
		return page, nil
	}
	var rows []passengerRow
	offset := (filter.Page - 1) * filter.PageSize
	query := applyAdminPassengerFilter(r.data.gormDB.WithContext(ctx).Table("passengers"), filter).
		Select(passengerAdminColumns)
	if err := query.Order("created_at DESC, id DESC").Limit(filter.PageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		page.Items = append(page.Items, rows[index].toBiz())
	}
	return page, nil
}

// GetPassengerAny 后台乘客详情，不校验归属。
func (r *rideRepo) GetPassengerAny(ctx context.Context, passengerID int64) (*biz.AdminPassenger, error) {
	var row passengerRow
	err := r.data.gormDB.WithContext(ctx).Table("passengers").
		Select(passengerAdminColumns).Where("id = ?", passengerID).Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound || isOptionalRelationError(err) {
			return nil, biz.ErrNotFound
		}
		return nil, err
	}
	return row.toBiz(), nil
}

func applyAdminPassengerFilter(query *gorm.DB, filter biz.AdminPassengerFilter) *gorm.DB {
	if filter.RealNameStatus != nil {
		query = query.Where("real_name_status = ?", *filter.RealNameStatus)
	}
	if filter.AccountStatus != nil {
		query = query.Where("account_status = ?", *filter.AccountStatus)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		if passengerID, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			query = query.Where("(passenger_no ILIKE ? OR mobile ILIKE ? OR nickname ILIKE ? OR id = ?)", like, like, like, passengerID)
		} else {
			query = query.Where("(passenger_no ILIKE ? OR mobile ILIKE ? OR nickname ILIKE ?)", like, like, like)
		}
	}
	return query
}
