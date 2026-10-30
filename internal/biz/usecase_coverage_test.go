// 模块1 用例层补充测试：个人中心聚合、偏好合并、地址列表/删除、消息已读、积分、令牌鉴权。
// 目标：把 biz 层覆盖率拉到 agent/05 §1 要求的 60% 以上，全部使用内存假实现。
package biz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const testPax int64 = 101

func newPassengerProfileFixture() (*PassengerUsecase, *fakePassengerRepo, *fakeCouponRepo, *fakeNotificationRepo, *fakePointRepo, *Passenger) {
	passengers := newFakePassengerRepo()
	pref := DefaultPreferences()
	passenger := &Passenger{
		ID:             testPax,
		Mobile:         "13800000101",
		Nickname:       "乘客0101",
		AccountStatus:  AccountStatusNormal,
		RealNameStatus: RealNameStatusNone,
		Preferences:    pref,
	}
	passengers.byID[passenger.ID] = passenger

	coupons := &fakeCouponRepo{items: []*Coupon{
		{ID: 1, CouponID: 1, CouponName: "立减券", CouponType: 1, Status: CouponStatusUnused, ExpiresAt: time.Now().Add(24 * time.Hour)},
		{ID: 2, CouponID: 2, CouponName: "过期券", CouponType: 1, Status: CouponStatusUnused, ExpiresAt: time.Now().Add(-24 * time.Hour)},
	}}
	notifications := &fakeNotificationRepo{items: []*Notification{
		{ID: 1, Title: "订单", NotificationType: 1},
		{ID: 2, Title: "活动", NotificationType: 3},
	}}
	today := StartOfDay(time.Now(), bizLocation)
	points := &fakePointRepo{
		balance: 50,
		signIns: map[string]*SignInRecord{
			today.Format("2006-01-02"): {PassengerID: testPax, SignInDate: today, RewardPoints: 5, ContinuousDays: 3},
		},
	}
	uc := NewPassengerUsecase(passengers, coupons, notifications, points, log.DefaultLogger)
	return uc, passengers, coupons, notifications, points, passenger
}

func TestGetProfileAggregates(t *testing.T) {
	uc, _, _, _, _, _ := newPassengerProfileFixture()

	profile, err := uc.GetProfile(context.Background(), testPax)
	if err != nil {
		t.Fatalf("聚合查询失败: %v", err)
	}
	if profile.Passenger == nil || profile.Passenger.ID != testPax {
		t.Fatal("未返回乘客资料")
	}
	if profile.WalletBalance != 12.34 { // 假实现固定返回 12.34
		t.Fatalf("钱包余额不符: %v", profile.WalletBalance)
	}
	if profile.AvailableCouponCount != 1 {
		t.Fatalf("可用券应只统计未过期未使用券，实际 %d", profile.AvailableCouponCount)
	}
	if profile.UnreadNotificationCount != 2 {
		t.Fatalf("未读消息数不符: %d", profile.UnreadNotificationCount)
	}
	if !profile.SignedInToday || profile.ContinuousSignInDays != 3 {
		t.Fatalf("签到摘要不符: today=%v days=%d", profile.SignedInToday, profile.ContinuousSignInDays)
	}
}

func TestGetProfilePassengerNotFound(t *testing.T) {
	uc, _, _, _, _, _ := newPassengerProfileFixture()

	if _, err := uc.GetProfile(context.Background(), 9999); err == nil {
		t.Fatal("不存在的乘客应返回错误")
	}
}

func TestUpdatePreferencesMergesAndKeepsSecurityOn(t *testing.T) {
	uc, passengers, _, _, _, _ := newPassengerProfileFixture()

	promotionOff := false
	securityOff := false
	merged, err := uc.UpdatePreferences(context.Background(), testPax, PassengerPreferences{
		PromotionNotificationEnabled: &promotionOff,
		SecurityNotificationEnabled:  &securityOff,
	})
	if err != nil {
		t.Fatalf("更新偏好失败: %v", err)
	}
	if merged.PromotionNotificationEnabled == nil || *merged.PromotionNotificationEnabled {
		t.Fatal("显式关闭活动通知应生效")
	}
	// 安全类通知属于强触达，传 false 时服务端忽略。
	if merged.SecurityNotificationEnabled == nil || !*merged.SecurityNotificationEnabled {
		t.Fatal("安全类通知不允许被关闭")
	}
	if !*passengers.byID[testPax].Preferences.OrderNotificationEnabled {
		t.Fatal("未传入的偏好项应保留原值")
	}
}

func TestAddressListAndDelete(t *testing.T) {
	repo := &fakeAddressRepo{items: map[int64]*Address{
		1: {ID: 1, PassengerID: testPax, AddressType: 1, AddressName: "家", Address: "望京SOHO"},
		2: {ID: 2, PassengerID: 999, AddressType: 2, AddressName: "公司", Address: "中关村"},
	}}
	uc := NewAddressUsecase(repo, log.DefaultLogger)

	res, err := uc.List(context.Background(), testPax, 0, 0, nil)
	if err != nil {
		t.Fatalf("地址列表失败: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("应只返回本人地址, total=%d items=%d", res.Total, len(res.Items))
	}
	if res.Page != DefaultPage || res.PageSize != DefaultPageSize {
		t.Fatalf("分页参数未归一化: page=%d size=%d", res.Page, res.PageSize)
	}

	// 非法地址类型
	badType := int32(9)
	if _, err := uc.List(context.Background(), testPax, 0, 0, &badType); err == nil {
		t.Fatal("非法地址类型应报参数错误")
	}

	// 越权删除按不存在处理
	if err := uc.Delete(context.Background(), 999, 1); err == nil {
		t.Fatal("删除他人地址应返回不存在")
	}
	if err := uc.Delete(context.Background(), testPax, 1); err != nil {
		t.Fatalf("删除本人地址失败: %v", err)
	}
	if _, ok := repo.items[1]; ok {
		t.Fatal("删除后地址仍存在")
	}
}

func TestNotificationListAndMarkRead(t *testing.T) {
	repo := &fakeNotificationRepo{items: []*Notification{
		{ID: 1, Title: "订单已支付", NotificationType: 2},
		{ID: 2, Title: "优惠券到账", NotificationType: 3},
	}}
	uc := NewNotificationUsecase(repo, log.DefaultLogger)

	res, err := uc.List(context.Background(), testPax, 0, 0, nil)
	if err != nil {
		t.Fatalf("消息列表失败: %v", err)
	}
	if res.Total != 2 || res.UnreadCount != 2 {
		t.Fatalf("消息统计不符: total=%d unread=%d", res.Total, res.UnreadCount)
	}

	// 既未指定 ids 也未 mark_all：参数错误
	if _, _, err := uc.MarkRead(context.Background(), testPax, nil, false); err == nil {
		t.Fatal("ids 为空且 mark_all=false 应报错")
	}
	// 单次超过 100 条：参数错误
	if _, _, err := uc.MarkRead(context.Background(), testPax, make([]int64, 101), false); err == nil {
		t.Fatal("超过 100 条应报错")
	}

	updated, unread, err := uc.MarkRead(context.Background(), testPax, []int64{1}, false)
	if err != nil {
		t.Fatalf("标记已读失败: %v", err)
	}
	if updated != 1 || unread != 1 {
		t.Fatalf("已读统计不符: updated=%d unread=%d", updated, unread)
	}

	if _, unread, err = uc.MarkRead(context.Background(), testPax, nil, true); err != nil {
		t.Fatalf("全部已读失败: %v", err)
	}
	if unread != 0 {
		t.Fatalf("全部已读后未读数应为 0，实际 %d", unread)
	}
}

func TestGetPointsUsecase(t *testing.T) {
	uc := NewPointUsecase(&fakePointRepo{balance: 50}, log.DefaultLogger)

	res, err := uc.GetPoints(context.Background(), testPax, 0, 100)
	if err != nil {
		t.Fatalf("积分查询失败: %v", err)
	}
	if res.PointBalance != 50 {
		t.Fatalf("积分余额不符: %d", res.PointBalance)
	}
	if res.Page != DefaultPage || res.PageSize != MaxPageSize {
		t.Fatalf("分页未归一化: page=%d size=%d", res.Page, res.PageSize)
	}
}

func TestAuthenticateToken(t *testing.T) {
	passengers := newFakePassengerRepo()
	passengers.byID[testPax] = &Passenger{ID: testPax, AccountStatus: AccountStatusNormal}
	uc := NewAuthUsecase(passengers, &fakeAuthRecordRepo{}, newFakeCodeStore(),
		&fakeSmsSender{}, fakeTokenIssuer{}, log.DefaultLogger)

	id, err := uc.Authenticate(context.Background(), "Bearer test-token")
	if err != nil || id != testPax {
		t.Fatalf("合法令牌应通过: id=%d err=%v", id, err)
	}

	if _, err := uc.Authenticate(context.Background(), ""); err == nil {
		t.Fatal("空令牌应拒绝")
	}
	if _, err := uc.Authenticate(context.Background(), "Bearer bad-token"); err == nil {
		t.Fatal("非法令牌应拒绝")
	}
	if _, err := uc.Authenticate(context.Background(), "Bearer test-token"); err != nil {
		t.Fatalf("重复校验应仍通过: %v", err)
	}
}

func TestAuthenticateRejectsFrozenAccount(t *testing.T) {
	passengers := newFakePassengerRepo()
	passengers.byID[testPax] = &Passenger{ID: testPax, AccountStatus: AccountStatusFrozen}
	uc := NewAuthUsecase(passengers, &fakeAuthRecordRepo{}, newFakeCodeStore(),
		&fakeSmsSender{}, fakeTokenIssuer{}, log.DefaultLogger)

	if _, err := uc.Authenticate(context.Background(), "Bearer test-token"); err == nil {
		t.Fatal("冻结账号的存量令牌应立即失效")
	} else if !strings.Contains(err.Error(), "冻结") {
		t.Fatalf("错误提示应为账号冻结，实际 %v", err)
	}
}

func TestProvideRealNameSalt(t *testing.T) {
	t.Setenv("RIDE_ID_CARD_SALT", "unit-test-salt")
	if got := string(ProvideRealNameSalt()); got != "unit-test-salt" {
		t.Fatalf("盐值读取不符: %q", got)
	}
}
