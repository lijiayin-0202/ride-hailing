// 模块1 用户账号与个人中心（成员A）
// 乘客账户领域模型与个人中心聚合用例。
// 数据表：passengers / passenger_wallets
package biz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/pkg/validate"
)

// PassengerPreferences 对应 passengers.preferences（JSONB）。
// 使用指针字段表达「未提交」，保证 PATCH 只更新显式传入的偏好项。
type PassengerPreferences struct {
	OrderNotificationEnabled     *bool `json:"order_notification_enabled,omitempty"`
	PromotionNotificationEnabled *bool `json:"promotion_notification_enabled,omitempty"`
	SecurityNotificationEnabled  *bool `json:"security_notification_enabled,omitempty"`
	TripShareDefaultEnabled      *bool `json:"trip_share_default_enabled,omitempty"`
}

// Passenger 乘客账户实体（passengers）。
type Passenger struct {
	ID             int64
	PassengerNo    string
	Mobile         string
	Nickname       string
	AvatarURL      string
	RealNameStatus int16
	AccountStatus  int16
	PointBalance   int32
	Preferences    PassengerPreferences
	LastLoginAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Profile 个人中心聚合结果：一次返回资料、实名状态、积分、券与未读消息摘要。
type Profile struct {
	// Passenger 乘客资料（含实名状态与积分余额）。
	Passenger *Passenger
	// WalletBalance 钱包余额（元），来自 passenger_wallets。
	WalletBalance float64
	// AvailableCouponCount 可用券张数（未使用且未过期）。
	AvailableCouponCount int32
	// UnreadNotificationCount 未读消息数。
	UnreadNotificationCount int32
	// SignedInToday 今日（北京时间自然日）是否已签到。
	SignedInToday bool
	// ContinuousSignInDays 连续签到天数。
	ContinuousSignInDays int32
}

// PassengerRepo 乘客账户数据访问接口（实现在 internal/data）。
type PassengerRepo interface {
	GetByMobile(ctx context.Context, mobile string) (*Passenger, error)
	GetByID(ctx context.Context, id int64) (*Passenger, error)
	// Create 创建乘客并同步初始化钱包记录（同一事务）。
	Create(ctx context.Context, p *Passenger) error
	UpdateLastLogin(ctx context.Context, id int64, at time.Time) error
	// UpdatePreferences 全量覆盖 passengers.preferences。
	UpdatePreferences(ctx context.Context, id int64, pref PassengerPreferences) error
	// UpdateNickname 修改用户名（昵称）。
	UpdateNickname(ctx context.Context, id int64, nickname string) error
	// UpdateMobile 更换绑定手机号；手机号唯一索引冲突返回 ErrBusinessConflict。
	UpdateMobile(ctx context.Context, id int64, mobile string) error
	// WalletBalance 返回 passenger_wallets.balance（元）。
	WalletBalance(ctx context.Context, id int64) (float64, error)
}

// PassengerUsecase 个人中心用例。
// 聚合依赖券/通知/积分仓库，避免前端分别请求多个明细接口。
type PassengerUsecase struct {
	repo             PassengerRepo
	couponRepo       CouponRepo
	notificationRepo NotificationRepo
	pointRepo        PointRepo
	log              *log.Helper
}

// NewPassengerUsecase 构造函数（wire 注入）。
func NewPassengerUsecase(
	repo PassengerRepo,
	couponRepo CouponRepo,
	notificationRepo NotificationRepo,
	pointRepo PointRepo,
	logger log.Logger,
) *PassengerUsecase {
	return &PassengerUsecase{
		repo:             repo,
		couponRepo:       couponRepo,
		notificationRepo: notificationRepo,
		pointRepo:        pointRepo,
		log:              log.NewHelper(log.With(logger, "module", "biz/passenger")),
	}
}

// Get 查询乘客账户，不存在返回 ErrPassengerNotFound。
func (uc *PassengerUsecase) Get(ctx context.Context, id int64) (*Passenger, error) {
	return uc.repo.GetByID(ctx, id)
}

// GetProfile 个人中心聚合查询。
// 说明：聚合读接口并发度低、单次查询量小，这里按顺序读取以保证实现简单可读；
// 若后续要求压测指标，再改为 errgroup 并发读取。
func (uc *PassengerUsecase) GetProfile(ctx context.Context, id int64) (*Profile, error) {
	passenger, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	profile := &Profile{Passenger: passenger}

	if balance, err := uc.repo.WalletBalance(ctx, id); err == nil {
		profile.WalletBalance = balance
	} else {
		uc.log.WithContext(ctx).Warnf("读取钱包余额失败 passenger_id=%d err=%v", id, err)
	}

	if count, err := uc.couponRepo.CountAvailable(ctx, id, time.Now()); err == nil {
		profile.AvailableCouponCount = count
	} else {
		uc.log.WithContext(ctx).Warnf("读取可用券数量失败 passenger_id=%d err=%v", id, err)
	}

	if count, err := uc.notificationRepo.CountUnread(ctx, id); err == nil {
		profile.UnreadNotificationCount = count
	} else {
		uc.log.WithContext(ctx).Warnf("读取未读消息数失败 passenger_id=%d err=%v", id, err)
	}

	// 今日签到摘要：以业务时区（北京时间）自然日为准。
	today := StartOfDay(time.Now(), bizLocation)
	if record, err := uc.pointRepo.LastSignIn(ctx, id, today); err == nil && record != nil {
		profile.SignedInToday = true
		profile.ContinuousSignInDays = record.ContinuousDays
	}

	return profile, nil
}

// UpdateNickname 修改用户名（昵称）。
// 只改展示名，不影响登录主体；校验在 pkg/validate 完成，业务码在这里补齐。
func (uc *PassengerUsecase) UpdateNickname(ctx context.Context, id int64, nickname string) (*Passenger, error) {
	normalized, err := validate.Nickname(nickname)
	if err != nil {
		return nil, invalidArgument("%s", err.Error())
	}
	if err := uc.repo.UpdateNickname(ctx, id, normalized); err != nil {
		return nil, err
	}
	passenger, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	uc.log.WithContext(ctx).Infof("乘客修改用户名 passenger_id=%d nickname=%s", id, normalized)
	return passenger, nil
}

// UpdatePreferences 合并式更新偏好：只覆盖显式传入的项。
func (uc *PassengerUsecase) UpdatePreferences(ctx context.Context, id int64, patch PassengerPreferences) (*PassengerPreferences, error) {
	passenger, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	merged := passenger.Preferences
	if patch.OrderNotificationEnabled != nil {
		merged.OrderNotificationEnabled = patch.OrderNotificationEnabled
	}
	if patch.PromotionNotificationEnabled != nil {
		merged.PromotionNotificationEnabled = patch.PromotionNotificationEnabled
	}
	// 安全类通知属于强触达能力，业务上不允许用户关闭，传入 false 时忽略。
	securityOn := true
	merged.SecurityNotificationEnabled = &securityOn
	if patch.TripShareDefaultEnabled != nil {
		merged.TripShareDefaultEnabled = patch.TripShareDefaultEnabled
	}

	if err := uc.repo.UpdatePreferences(ctx, id, merged); err != nil {
		return nil, err
	}
	return &merged, nil
}

// DefaultPreferences 新用户默认偏好：订单/活动/安全通知开启，行程分享默认关闭。
func DefaultPreferences() PassengerPreferences {
	on := true
	off := false
	return PassengerPreferences{
		OrderNotificationEnabled:     &on,
		PromotionNotificationEnabled: &on,
		SecurityNotificationEnabled:  &on,
		TripShareDefaultEnabled:      &off,
	}
}
