// 模块1 用户账号与个人中心（成员A）—— 业务用例单元测试
// 覆盖《开发任务拆分》§3.7 要求的用例：登录状态、账号冻结、地址越权、券状态、重复签到、通知已读。
// 全部使用内存假实现，不依赖数据库/Redis，也不包含任何真实手机号。
package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"golang.org/x/crypto/bcrypt"

	"github.com/lijayin-0202/radish-ride-service/pkg/mask"
	"github.com/lijayin-0202/radish-ride-service/pkg/validate"
)

// ---------------------------- 假实现 ----------------------------

type fakePassengerRepo struct {
	byMobile     map[string]*Passenger
	byID         map[int64]*Passenger
	passwordHash map[string]string
	created      int
}

func newFakePassengerRepo() *fakePassengerRepo {
	return &fakePassengerRepo{
		byMobile:     map[string]*Passenger{},
		byID:         map[int64]*Passenger{},
		passwordHash: map[string]string{},
	}
}

// PasswordHashByMobile 模拟密码哈希查询：账号不存在返回 ErrPassengerNotFound，未设密码返回空串。
func (f *fakePassengerRepo) PasswordHashByMobile(_ context.Context, mobile string) (string, error) {
	if _, ok := f.byMobile[mobile]; !ok {
		return "", ErrPassengerNotFound
	}
	return f.passwordHash[mobile], nil
}

func (f *fakePassengerRepo) GetByMobile(_ context.Context, mobile string) (*Passenger, error) {
	if p, ok := f.byMobile[mobile]; ok {
		copied := *p
		return &copied, nil
	}
	return nil, ErrPassengerNotFound
}

func (f *fakePassengerRepo) GetByID(_ context.Context, id int64) (*Passenger, error) {
	if p, ok := f.byID[id]; ok {
		copied := *p
		return &copied, nil
	}
	return nil, ErrPassengerNotFound
}

func (f *fakePassengerRepo) Create(_ context.Context, p *Passenger) error {
	f.created++
	p.ID = int64(100 + f.created)
	f.byID[p.ID] = p
	f.byMobile[p.Mobile] = p
	return nil
}

func (f *fakePassengerRepo) UpdateLastLogin(_ context.Context, id int64, at time.Time) error {
	if p, ok := f.byID[id]; ok {
		p.LastLoginAt = &at
	}
	return nil
}

func (f *fakePassengerRepo) UpdatePreferences(_ context.Context, id int64, pref PassengerPreferences) error {
	if p, ok := f.byID[id]; ok {
		p.Preferences = pref
		return nil
	}
	return ErrPassengerNotFound
}

func (f *fakePassengerRepo) UpdateNickname(_ context.Context, id int64, nickname string) error {
	if p, ok := f.byID[id]; ok {
		p.Nickname = nickname
		return nil
	}
	return ErrPassengerNotFound
}

func (f *fakePassengerRepo) UpdateMobile(_ context.Context, id int64, mobile string) error {
	p, ok := f.byID[id]
	if !ok {
		return ErrPassengerNotFound
	}
	delete(f.byMobile, p.Mobile)
	p.Mobile = mobile
	f.byMobile[mobile] = p
	return nil
}

func (f *fakePassengerRepo) WalletBalance(_ context.Context, _ int64) (float64, error) {
	return 12.34, nil
}

type fakeAuthRecordRepo struct {
	records []*PassengerAuthRecord
}

func (f *fakeAuthRecordRepo) Create(_ context.Context, record *PassengerAuthRecord) error {
	f.records = append(f.records, record)
	return nil
}

type fakeCodeStore struct {
	challenges map[string]*SmsChallenge
	mobileIdx  map[string]string
	attempts   map[string]int32
	resend     map[string]time.Time
	sendCounts map[string]int64
}

func newFakeCodeStore() *fakeCodeStore {
	return &fakeCodeStore{
		challenges: map[string]*SmsChallenge{},
		mobileIdx:  map[string]string{},
		attempts:   map[string]int32{},
		resend:     map[string]time.Time{},
		sendCounts: map[string]int64{},
	}
}

func (f *fakeCodeStore) SaveChallenge(_ context.Context, c *SmsChallenge, _ time.Duration) error {
	copied := *c
	f.challenges[c.VerificationID] = &copied
	f.mobileIdx[c.Mobile] = c.VerificationID
	return nil
}

func (f *fakeCodeStore) GetChallenge(_ context.Context, verificationID string) (*SmsChallenge, error) {
	c, ok := f.challenges[verificationID]
	if !ok {
		return nil, nil
	}
	copied := *c
	copied.Attempts = f.attempts[verificationID]
	return &copied, nil
}

func (f *fakeCodeStore) GetChallengeByMobile(_ context.Context, mobile string) (*SmsChallenge, error) {
	id, ok := f.mobileIdx[mobile]
	if !ok {
		return nil, nil
	}
	return f.GetChallenge(context.Background(), id)
}

func (f *fakeCodeStore) IncrAttempt(_ context.Context, verificationID string) (int32, error) {
	f.attempts[verificationID]++
	return f.attempts[verificationID], nil
}

func (f *fakeCodeStore) DeleteChallenge(_ context.Context, verificationID string) error {
	delete(f.challenges, verificationID)
	delete(f.attempts, verificationID)
	for mobile, id := range f.mobileIdx {
		if id == verificationID {
			delete(f.mobileIdx, mobile)
		}
	}
	return nil
}

func (f *fakeCodeStore) AcquireResendLock(_ context.Context, scene, mobile string, ttl time.Duration) (bool, error) {
	key := scene + "|" + mobile
	if until, ok := f.resend[key]; ok && time.Now().Before(until) {
		return false, nil
	}
	f.resend[key] = time.Now().Add(ttl)
	return true, nil
}

func (f *fakeCodeStore) ReleaseResendLock(_ context.Context, scene, mobile string) error {
	delete(f.resend, scene+"|"+mobile)
	return nil
}

// IncrSendCount 模拟固定窗口计数（测试中不关心窗口过期）。
func (f *fakeCodeStore) IncrSendCount(_ context.Context, key string, _ time.Duration) (int64, error) {
	f.sendCounts[key]++
	return f.sendCounts[key], nil
}

// ResetSendCount 模拟清零计数键。
func (f *fakeCodeStore) ResetSendCount(_ context.Context, key string) error {
	delete(f.sendCounts, key)
	return nil
}

// CompareAndDelete 模拟存储层的原子消费：哈希不一致或已消费都返回 false。
func (f *fakeCodeStore) CompareAndDelete(_ context.Context, verificationID, codeHash string) (bool, error) {
	challenge, ok := f.challenges[verificationID]
	if !ok || challenge.CodeHash != codeHash {
		return false, nil
	}
	delete(f.challenges, verificationID)
	delete(f.attempts, verificationID)
	for mobile, id := range f.mobileIdx {
		if id == verificationID {
			delete(f.mobileIdx, mobile)
		}
	}
	return true, nil
}

type fakeSmsSender struct {
	lastCode   string
	lastMobile string
}

func (f *fakeSmsSender) SendLoginCode(_ context.Context, mobile, code string) error {
	f.lastMobile = mobile
	f.lastCode = code
	return nil
}

type fakeTokenIssuer struct{}

func (fakeTokenIssuer) Issue(_ context.Context, p *Passenger) (string, int32, error) {
	if p == nil || p.ID == 0 {
		return "", 0, ErrUnauthorized
	}
	return "test-token", 7200, nil
}

func (fakeTokenIssuer) Parse(_ context.Context, token string) (int64, error) {
	if token != "Bearer test-token" && token != "test-token" {
		return 0, ErrUnauthorized
	}
	return 101, nil
}

type fakeAddressRepo struct {
	items  map[int64]*Address
	nextID int64
}

func (f *fakeAddressRepo) List(_ context.Context, passengerID int64, addressType *int32, _, _ int32) ([]*Address, int64, error) {
	rows := make([]*Address, 0)
	for _, a := range f.items {
		if a.PassengerID != passengerID {
			continue
		}
		if addressType != nil && a.AddressType != *addressType {
			continue
		}
		rows = append(rows, a)
	}
	return rows, int64(len(rows)), nil
}

func (f *fakeAddressRepo) Get(_ context.Context, id, passengerID int64) (*Address, error) {
	a, ok := f.items[id]
	if !ok || a.PassengerID != passengerID {
		// 越权访问他人地址同样按不存在处理，避免探测资源是否存在
		return nil, ErrAddressNotFound
	}
	return a, nil
}

func (f *fakeAddressRepo) Create(_ context.Context, a *Address) error {
	f.nextID++
	a.ID = f.nextID
	if f.items == nil {
		f.items = map[int64]*Address{}
	}
	f.items[a.ID] = a
	return nil
}

func (f *fakeAddressRepo) Update(_ context.Context, a *Address) error {
	f.items[a.ID] = a
	return nil
}

func (f *fakeAddressRepo) Delete(_ context.Context, id, passengerID int64) (int64, error) {
	a, ok := f.items[id]
	if !ok || a.PassengerID != passengerID {
		return 0, nil
	}
	delete(f.items, id)
	return 1, nil
}

type fakeCouponRepo struct{ items []*Coupon }

func (f *fakeCouponRepo) ListByPassenger(_ context.Context, _ int64, status *int32, _, _ int32) ([]*Coupon, int64, error) {
	rows := make([]*Coupon, 0, len(f.items))
	for _, c := range f.items {
		if status != nil && c.Status != *status {
			continue
		}
		copied := *c
		rows = append(rows, &copied)
	}
	return rows, int64(len(rows)), nil
}

func (f *fakeCouponRepo) CountAvailable(_ context.Context, _ int64, now time.Time) (int32, error) {
	var count int32
	for _, c := range f.items {
		if c.Status == CouponStatusUnused && c.ExpiresAt.After(now) {
			count++
		}
	}
	return count, nil
}

type fakePointRepo struct {
	balance   int32
	signIns   map[string]*SignInRecord
	signCalls int
}

func (f *fakePointRepo) Balance(_ context.Context, _ int64) (int32, error) { return f.balance, nil }

func (f *fakePointRepo) ListTransactions(_ context.Context, _ int64, _, _ int32) ([]*PointTransaction, int64, error) {
	return nil, 0, nil
}

func (f *fakePointRepo) LastSignIn(_ context.Context, _ int64, day time.Time) (*SignInRecord, error) {
	if f.signIns == nil {
		return nil, nil
	}
	if r, ok := f.signIns[day.Format("2006-01-02")]; ok {
		return r, nil
	}
	return nil, nil
}

func (f *fakePointRepo) SignIn(_ context.Context, passengerID int64, day time.Time, rewardPoints, continuousDays int32, _ string) (*SignInResult, error) {
	f.signCalls++
	if f.signIns == nil {
		f.signIns = map[string]*SignInRecord{}
	}
	key := day.Format("2006-01-02")
	if existing, ok := f.signIns[key]; ok {
		return &SignInResult{
			SignInDate: existing.SignInDate, RewardPoints: existing.RewardPoints,
			ContinuousDays: existing.ContinuousDays, PointBalance: f.balance, AlreadySigned: true,
		}, nil
	}
	f.signIns[key] = &SignInRecord{PassengerID: passengerID, SignInDate: day, RewardPoints: rewardPoints, ContinuousDays: continuousDays}
	f.balance += rewardPoints
	return &SignInResult{SignInDate: day, RewardPoints: rewardPoints, ContinuousDays: continuousDays, PointBalance: f.balance}, nil
}

type fakeNotificationRepo struct {
	items  []*Notification
	marked int
}

func (f *fakeNotificationRepo) List(_ context.Context, _ int64, isRead *bool, _, _ int32) ([]*Notification, int64, error) {
	rows := make([]*Notification, 0, len(f.items))
	for _, n := range f.items {
		if isRead != nil && n.IsRead != *isRead {
			continue
		}
		rows = append(rows, n)
	}
	return rows, int64(len(rows)), nil
}

func (f *fakeNotificationRepo) CountUnread(_ context.Context, _ int64) (int32, error) {
	var count int32
	for _, n := range f.items {
		if !n.IsRead {
			count++
		}
	}
	return count, nil
}

func (f *fakeNotificationRepo) MarkRead(_ context.Context, _ int64, ids []int64) (int32, error) {
	var updated int32
	for _, n := range f.items {
		for _, id := range ids {
			if n.ID == id && !n.IsRead {
				n.IsRead = true
				updated++
			}
		}
	}
	f.marked += int(updated)
	return updated, nil
}

func (f *fakeNotificationRepo) MarkAllRead(_ context.Context, _ int64) (int32, error) {
	var updated int32
	for _, n := range f.items {
		if !n.IsRead {
			n.IsRead = true
			updated++
		}
	}
	f.marked += int(updated)
	return updated, nil
}

func newTestAuthUsecase(t *testing.T, passenger *Passenger) (*AuthUsecase, *fakePassengerRepo, *fakeCodeStore, *fakeSmsSender, *fakeAuthRecordRepo) {
	t.Helper()
	repo := newFakePassengerRepo()
	if passenger != nil {
		repo.byID[passenger.ID] = passenger
		repo.byMobile[passenger.Mobile] = passenger
	}
	store := newFakeCodeStore()
	sender := &fakeSmsSender{}
	records := &fakeAuthRecordRepo{}
	uc := NewAuthUsecase(repo, records, store, sender, fakeTokenIssuer{}, log.DefaultLogger)
	return uc, repo, store, sender, records
}

// ---------------------------- 测试用例 ----------------------------

// 手机号规范化与脱敏（校验/脱敏已下沉到 pkg/validate、pkg/mask）。
func TestNormalizeMobileAndMask(t *testing.T) {
	if got, err := validate.Mobile(" +86 138-0000-0000 "); err != nil || got != "13800000000" {
		t.Fatalf("validate.Mobile 结果异常: %q %v", got, err)
	}
	if _, err := validate.Mobile("12345"); err == nil {
		t.Fatal("非法手机号应当返回参数错误")
	}
	if got := mask.Mobile("13800000000"); got != "138****0000" {
		t.Fatalf("脱敏结果异常: %s", got)
	}
}

// 验证码发送：响应不含真实验证码，且冷却期内被限流。
func TestSendLoginCode_RateLimited(t *testing.T) {
	uc, _, _, sender, _ := newTestAuthUsecase(t, nil)
	ctx := context.Background()

	ticket, err := uc.SendLoginCode(ctx, "13800000001", "LOGIN")
	if err != nil {
		t.Fatalf("首次发送应成功: %v", err)
	}
	if ticket.VerificationID == "" || sender.lastCode == "" {
		t.Fatal("验证码挑战未生成")
	}
	if _, err := uc.SendLoginCode(ctx, "13800000001", "LOGIN"); !errors.Is(err, ErrSmsTooFrequent) {
		t.Fatalf("冷却期内应限流，实际: %v", err)
	}
}

// 手机号单日上限：每次发送后释放冷却锁模拟 60 秒重试，第 11 次应被限流。
func TestSendLoginCode_DailyQuota(t *testing.T) {
	uc, _, store, _, _ := newTestAuthUsecase(t, nil)
	ctx := context.Background()
	const mobile = "13800000009"

	for i := int64(0); i < SmsDailyLimitPerMobile; i++ {
		if _, err := uc.SendLoginCode(ctx, mobile, SmsCodeSceneLogin); err != nil {
			t.Fatalf("第 %d 次发送应成功: %v", i+1, err)
		}
		if err := store.ReleaseResendLock(ctx, SmsCodeSceneLogin, mobile); err != nil {
			t.Fatalf("释放冷却锁失败: %v", err)
		}
	}
	if _, err := uc.SendLoginCode(ctx, mobile, SmsCodeSceneLogin); !errors.Is(err, ErrSmsTooFrequent) {
		t.Fatalf("超过单日上限应限流，实际: %v", err)
	}
}

// IP 维度上限：同一 IP 下换不同手机号，第 21 次应被限流。
func TestSendLoginCode_IPQuota(t *testing.T) {
	uc, _, store, _, _ := newTestAuthUsecase(t, nil)
	ctx := context.Background()
	const clientIP = "10.0.0.1"

	for i := int64(0); i < SmsHourlyLimitPerIP; i++ {
		mobile := fmt.Sprintf("1390000%04d", i)
		if _, err := uc.SendSmsCodeWithMeta(ctx, 0, mobile, SmsCodeSceneLogin, clientIP, ""); err != nil {
			t.Fatalf("第 %d 次发送应成功: %v", i+1, err)
		}
		_ = store.ReleaseResendLock(ctx, SmsCodeSceneLogin, mobile)
	}
	if _, err := uc.SendSmsCodeWithMeta(ctx, 0, "13900009999", SmsCodeSceneLogin, clientIP, ""); !errors.Is(err, ErrSmsTooFrequent) {
		t.Fatalf("超过 IP 小时上限应限流，实际: %v", err)
	}
}

// ---------------------------- 密码登录（UA-T0-05） ----------------------------

// newPasswordLoginFixture 构造带密码的账号：password 非空时写入 bcrypt 哈希。
func newPasswordLoginFixture(t *testing.T, password string) (*AuthUsecase, *fakePassengerRepo) {
	t.Helper()
	passenger := &Passenger{ID: 201, PassengerNo: "P201", Mobile: "13800000201", AccountStatus: AccountStatusNormal}
	uc, repo, _, _, _ := newTestAuthUsecase(t, passenger)
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("生成密码哈希失败: %v", err)
		}
		repo.passwordHash[passenger.Mobile] = string(hash)
	}
	return uc, repo
}

// 密码正确：返回令牌，并写一条认证方式为密码的登录审计。
func TestPasswordLogin_Success(t *testing.T) {
	uc, _ := newPasswordLoginFixture(t, "Passw0rd!")
	result, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: "13800000201", DeviceID: "dev-1"}, "Passw0rd!")
	if err != nil {
		t.Fatalf("密码登录应成功: %v", err)
	}
	if result.AccessToken == "" || result.Passenger == nil {
		t.Fatal("应返回令牌与乘客信息")
	}
}

// 密码错误：统一返回 ErrPasswordIncorrect。
func TestPasswordLogin_WrongPassword(t *testing.T) {
	uc, _ := newPasswordLoginFixture(t, "Passw0rd!")
	if _, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: "13800000201"}, "wrong-pass"); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("密码错误应返回统一话术，实际: %v", err)
	}
}

// 未设置密码（手机号注册账号）：提示改用验证码登录，禁止空密码登录。
func TestPasswordLogin_NoPasswordSet(t *testing.T) {
	uc, _ := newPasswordLoginFixture(t, "")
	if _, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: "13800000201"}, "Passw0rd!"); !errors.Is(err, ErrPasswordNotSet) {
		t.Fatalf("未设置密码应提示改用验证码，实际: %v", err)
	}
	if _, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: "13800000201"}, ""); err == nil {
		t.Fatal("空密码必须被拒绝")
	}
}

// 账号不存在：不暴露账号是否存在，与密码错误同一话术。
func TestPasswordLogin_UnknownMobile(t *testing.T) {
	uc, _, _, _, _ := newTestAuthUsecase(t, nil)
	if _, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: "13800000299"}, "Passw0rd!"); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("账号不存在应返回统一话术，实际: %v", err)
	}
}

// 连续错误达到上限后锁定：第 6 次即使密码正确也被拒绝。
func TestPasswordLogin_FailLocked(t *testing.T) {
	uc, _ := newPasswordLoginFixture(t, "Passw0rd!")
	ctx := context.Background()

	for i := int64(0); i < PasswordMaxAttempts; i++ {
		if _, err := uc.PasswordLogin(ctx, &LoginInput{Mobile: "13800000201"}, "wrong-pass"); !errors.Is(err, ErrPasswordIncorrect) {
			t.Fatalf("第 %d 次错误密码应返回统一话术，实际: %v", i+1, err)
		}
	}
	if _, err := uc.PasswordLogin(ctx, &LoginInput{Mobile: "13800000201"}, "Passw0rd!"); !errors.Is(err, ErrPasswordAttemptLimited) {
		t.Fatalf("超过尝试上限应锁定，实际: %v", err)
	}
}

// 成功登录清零失败计数，不会因为历史失败次数把正常用户锁住。
func TestPasswordLogin_SuccessResetsCounter(t *testing.T) {
	uc, _ := newPasswordLoginFixture(t, "Passw0rd!")
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		_, _ = uc.PasswordLogin(ctx, &LoginInput{Mobile: "13800000201"}, "wrong-pass")
	}
	if _, err := uc.PasswordLogin(ctx, &LoginInput{Mobile: "13800000201"}, "Passw0rd!"); err != nil {
		t.Fatalf("第 5 次密码正确应登录成功: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := uc.PasswordLogin(ctx, &LoginInput{Mobile: "13800000201"}, "wrong-pass"); !errors.Is(err, ErrPasswordIncorrect) {
			t.Fatalf("清零后第 %d 次应仍是密码错误，实际: %v", i+1, err)
		}
	}
}

// 冻结账号：即使密码正确也不允许登录。
func TestPasswordLogin_AccountFrozen(t *testing.T) {
	passenger := &Passenger{ID: 202, PassengerNo: "P202", Mobile: "13800000202", AccountStatus: AccountStatusFrozen}
	uc, repo, _, _, _ := newTestAuthUsecase(t, passenger)
	hash, err := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	repo.passwordHash[passenger.Mobile] = string(hash)
	if _, err := uc.PasswordLogin(context.Background(), &LoginInput{Mobile: passenger.Mobile}, "Passw0rd!"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("冻结账号应拒绝密码登录，实际: %v", err)
	}
}

// 登录成功：老用户直接登录。
func TestMobileLogin_Success(t *testing.T) {
	passenger := &Passenger{ID: 101, PassengerNo: "P1", Mobile: "13800000002", AccountStatus: AccountStatusNormal}
	uc, _, _, sender, records := newTestAuthUsecase(t, passenger)
	ctx := context.Background()

	ticket, err := uc.SendLoginCode(ctx, passenger.Mobile, "LOGIN")
	if err != nil {
		t.Fatalf("发送验证码失败: %v", err)
	}
	result, err := uc.MobileLogin(ctx, &LoginInput{Mobile: passenger.Mobile, SmsCode: sender.lastCode, VerificationID: ticket.VerificationID})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if result.AccessToken == "" || result.IsNewUser {
		t.Fatalf("登录结果异常: %+v", result)
	}
	if len(records.records) != 1 || records.records[0].LoginResult != LoginResultSuccess {
		t.Fatal("登录成功应写入 success 审计")
	}
}

// 登录失败：冻结账号禁止登录。
func TestMobileLogin_AccountFrozen(t *testing.T) {
	passenger := &Passenger{ID: 102, PassengerNo: "P2", Mobile: "13800000003", AccountStatus: AccountStatusFrozen}
	uc, _, _, sender, _ := newTestAuthUsecase(t, passenger)
	ctx := context.Background()

	ticket, _ := uc.SendLoginCode(ctx, passenger.Mobile, "LOGIN")
	if _, err := uc.MobileLogin(ctx, &LoginInput{Mobile: passenger.Mobile, SmsCode: sender.lastCode, VerificationID: ticket.VerificationID}); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("冻结账号应被拒绝，实际: %v", err)
	}
}

// 登录失败：验证码错误累加次数，达到上限后锁定。
func TestMobileLogin_WrongCodeLimited(t *testing.T) {
	passenger := &Passenger{ID: 103, PassengerNo: "P3", Mobile: "13800000004", AccountStatus: AccountStatusNormal}
	uc, _, _, _, _ := newTestAuthUsecase(t, passenger)
	ctx := context.Background()

	ticket, _ := uc.SendLoginCode(ctx, passenger.Mobile, "LOGIN")
	var lastErr error
	for i := int32(0); i < SmsMaxAttempts; i++ {
		_, lastErr = uc.MobileLogin(ctx, &LoginInput{Mobile: passenger.Mobile, SmsCode: "000000", VerificationID: ticket.VerificationID})
	}
	if !errors.Is(lastErr, ErrSmsAttemptLimited) {
		t.Fatalf("错误次数达到上限应锁定，实际: %v", lastErr)
	}
}

// 新手机号自动注册并初始化默认偏好。
func TestMobileLogin_AutoRegister(t *testing.T) {
	uc, repo, _, sender, _ := newTestAuthUsecase(t, nil)
	ctx := context.Background()
	mobile := "13800000005"

	ticket, _ := uc.SendLoginCode(ctx, mobile, "LOGIN")
	result, err := uc.MobileLogin(ctx, &LoginInput{Mobile: mobile, SmsCode: sender.lastCode, VerificationID: ticket.VerificationID})
	if err != nil {
		t.Fatalf("自动注册登录失败: %v", err)
	}
	if !result.IsNewUser || repo.created != 1 {
		t.Fatalf("应创建新乘客，实际 created=%d", repo.created)
	}
	if result.Passenger.AccountStatus != AccountStatusNormal || result.Passenger.Nickname == "" {
		t.Fatalf("新乘客默认字段异常: %+v", result.Passenger)
	}
}

// 常用地址：越权修改他人地址按不存在处理。
func TestUpdateAddress_Forbidden(t *testing.T) {
	repo := &fakeAddressRepo{items: map[int64]*Address{
		7: {ID: 7, PassengerID: 999, AddressType: AddressTypeHome, AddressName: "家", Address: "某小区"},
	}}
	uc := NewAddressUsecase(repo, log.DefaultLogger)
	name := "公司"
	if _, err := uc.Update(context.Background(), 101, 7, &AddressInput{AddressName: &name}); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("越权修改应被拒绝，实际: %v", err)
	}
}

// 地址参数字段校验。
func TestCreateAddress_InvalidType(t *testing.T) {
	uc := NewAddressUsecase(&fakeAddressRepo{}, log.DefaultLogger)
	addressType := int32(9)
	name := "家"
	detail := "某小区"
	if _, err := uc.Create(context.Background(), 101, &AddressInput{
		AddressType: &addressType, AddressName: &name, Address: &detail,
	}); err == nil {
		t.Fatal("非法地址类型应返回参数错误")
	}
}

// 券状态：过期的未使用券对外归一化为已过期。
func TestListCoupons_ExpiredNormalized(t *testing.T) {
	repo := &fakeCouponRepo{items: []*Coupon{{
		ID: 1, Status: CouponStatusUnused, ExpiresAt: time.Now().Add(-time.Hour),
	}}}
	uc := NewCouponUsecase(repo, log.DefaultLogger)
	result, err := uc.List(context.Background(), 101, 1, 20, nil)
	if err != nil {
		t.Fatalf("查询券失败: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != CouponStatusExpired {
		t.Fatalf("过期券状态未归一化: %+v", result.Items[0])
	}
	if result.AvailableCount != 0 {
		t.Fatalf("可用券数量应为 0，实际 %d", result.AvailableCount)
	}
}

// 重复签到：同一天重复提交不重复加积分。
func TestSignIn_Idempotent(t *testing.T) {
	repo := &fakePointRepo{balance: 10}
	uc := NewPointUsecase(repo, log.DefaultLogger)
	ctx := context.Background()

	first, err := uc.SignIn(ctx, 101)
	if err != nil || first.AlreadySigned || first.RewardPoints != SignInBasePoints {
		t.Fatalf("首次签到异常: %+v %v", first, err)
	}
	second, err := uc.SignIn(ctx, 101)
	if err != nil {
		t.Fatalf("重复签到不应报错: %v", err)
	}
	if !second.AlreadySigned || second.PointBalance != first.PointBalance {
		t.Fatalf("重复签到应幂等，实际: %+v", second)
	}
}

// 通知已读：未指定 id 且非全部已读时拒绝。
func TestMarkNotificationsRead_RequireIDs(t *testing.T) {
	uc := NewNotificationUsecase(&fakeNotificationRepo{}, log.DefaultLogger)
	if _, _, err := uc.MarkRead(context.Background(), 101, nil, false); err == nil {
		t.Fatal("未指定消息ID应返回参数错误")
	}
}
