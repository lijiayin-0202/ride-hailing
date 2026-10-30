// 模块1 用户账号与个人中心（成员A）
// 我的优惠券聚合查询：passenger_coupons（持券实例）联表 coupons（券模板）。
// 说明：券的发放/核销属于模块2（下单锁券）与营销域，本模块只做只读聚合。
package biz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// 用户券状态（passenger_coupons.status）
const (
	CouponStatusUnused  int32 = 0 // 未使用
	CouponStatusLocked  int32 = 1 // 已锁定（下单占用）
	CouponStatusUsed    int32 = 2 // 已使用
	CouponStatusExpired int32 = 3 // 已过期
)

// 券类型（coupons.coupon_type）
const (
	CouponTypeInstant  int32 = 1 // 立减
	CouponTypeFullCut  int32 = 2 // 满减
	CouponTypeDiscount int32 = 3 // 折扣
)

// Coupon 持券实例 + 券模板聚合结果。
type Coupon struct {
	// ID 持券实例主键（passenger_coupons.id）。
	ID int64
	// CouponID 券模板主键（coupons.id）。
	CouponID int64
	// CouponInstanceNo 券实例编号，业务唯一。
	CouponInstanceNo string
	// CouponName 券名称（取自模板）。
	CouponName string
	// CouponType 券类型：1 立减 / 2 满减 / 3 折扣。
	CouponType int32
	// DiscountAmount 立减/满减金额（元）。
	DiscountAmount float64
	// DiscountRate 折扣率（coupon_type=3 时有效）。
	DiscountRate float64
	// MinimumOrderAmount 使用门槛金额（元）。
	MinimumOrderAmount float64
	// ServiceAreaID 适用运营区域；nil 表示不限区域。
	ServiceAreaID *int64
	// ValidFrom 券模板生效时间。
	ValidFrom time.Time
	// ValidTo 券模板失效时间。
	ValidTo time.Time
	// Status 持券状态：0 未使用 / 1 已锁定 / 2 已使用 / 3 已过期。
	Status int32
	// ReceivedAt 领取时间。
	ReceivedAt time.Time
	// ExpiresAt 过期时间（实例维度，优先于模板 ValidTo）。
	ExpiresAt time.Time
}

// CouponListResult 分页结果。
type CouponListResult struct {
	// Items 当前页券列表。
	Items []*Coupon
	// Total 满足筛选条件的总数。
	Total int64
	// Page 归一化后的页码。
	Page int32
	// PageSize 归一化后的每页条数。
	PageSize int32
	// AvailableCount 可用券张数（未使用且未过期），与分页无关。
	AvailableCount int32
}

// CouponRepo 优惠券数据访问接口。
type CouponRepo interface {
	ListByPassenger(ctx context.Context, passengerID int64, status *int32, offset, limit int32) ([]*Coupon, int64, error)
	// CountAvailable 统计未使用且在有效期内的券数量。
	CountAvailable(ctx context.Context, passengerID int64, now time.Time) (int32, error)
}

// CouponUsecase 优惠券用例。
type CouponUsecase struct {
	repo CouponRepo
	log  *log.Helper
}

// NewCouponUsecase 构造函数（wire 注入）。
func NewCouponUsecase(repo CouponRepo, logger log.Logger) *CouponUsecase {
	return &CouponUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "biz/coupon")),
	}
}

// List 分页查询当前乘客的券包。
// 过期状态归一化：库里仍是「未使用」但已过有效期的券，对外统一展示为已过期。
func (uc *CouponUsecase) List(ctx context.Context, passengerID int64, page, pageSize int32, status *int32) (*CouponListResult, error) {
	// 状态只允许文档定义的四个枚举值，防止前端传入非法筛选条件。
	if status != nil && *status != CouponStatusUnused && *status != CouponStatusLocked &&
		*status != CouponStatusUsed && *status != CouponStatusExpired {
		return nil, invalidArgument("券状态不合法")
	}
	// 分页参数归一化（默认 20、上限 50），列表接口禁止返回全量。
	page, pageSize, offset := NormalizePage(page, pageSize)
	items, total, err := uc.repo.ListByPassenger(ctx, passengerID, status, offset, pageSize)
	if err != nil {
		return nil, err
	}
	// 过期归一化：库里仍是「未使用」但已过期的券，对外展示为已过期。
	now := time.Now()
	for _, item := range items {
		if item.Status == CouponStatusUnused && now.After(item.ExpiresAt) {
			item.Status = CouponStatusExpired
		}
	}
	// 可用券计数失败不阻断列表（降级为 0 并告警）。
	available, err := uc.repo.CountAvailable(ctx, passengerID, now)
	if err != nil {
		uc.log.WithContext(ctx).Warnf("统计可用券数量失败 passenger_id=%d err=%v", passengerID, err)
	}
	return &CouponListResult{Items: items, Total: total, Page: page, PageSize: pageSize, AvailableCount: available}, nil
}
