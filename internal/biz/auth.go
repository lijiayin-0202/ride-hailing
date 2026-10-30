// 模块1 用户账号与个人中心（成员A）—— T0 鉴权支撑
// 手机号验证码登录：验证码只保存哈希、一次性消费；令牌结构由 TokenIssuer 抽象，
// 后续接入模块6统一认证设施时只需替换实现。
package biz

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/pkg/mask"
	"github.com/lijayin-0202/radish-ride-service/pkg/validate"
)

// 验证码与令牌相关常量。
// 验证码按场景隔离：不同场景的挑战不能互相复用（Scene 参与校验），避免拿换绑验证码去登录。
const (
	// SmsCodeSceneLogin 登录验证码。
	SmsCodeSceneLogin = "LOGIN"
	// SmsCodeSceneChangeOldMobile 换绑手机号：验证原手机号。
	SmsCodeSceneChangeOldMobile = "CHANGE_MOBILE_OLD"
	// SmsCodeSceneChangeNewMobile 换绑手机号：验证新手机号。
	SmsCodeSceneChangeNewMobile = "CHANGE_MOBILE_NEW"

	// SmsCodeTTL 验证码有效期：5 分钟。
	SmsCodeTTL = 5 * time.Minute
	// SmsResendInterval 同一手机号发送冷却期：60 秒（后端限流为准）。
	SmsResendInterval = 60 * time.Second
	// SmsMaxAttempts 单个挑战最多校验 5 次，超限作废。
	SmsMaxAttempts int32 = 5
	// AccessTokenTTL 访问令牌有效期：2 小时。
	AccessTokenTTL = 2 * time.Hour

	// 认证方式（passenger_auth_records.auth_type）
	// AuthTypeMobileSms 手机号验证码登录。
	AuthTypeMobileSms int16 = 1
	// AuthTypeMobileChange 更换绑定手机号。
	AuthTypeMobileChange int16 = 2
	// 登录结果（passenger_auth_records.login_result）
	// LoginResultFailed 登录失败（含验证码错误、账号冻结等）。
	LoginResultFailed int16 = 0
	// LoginResultSuccess 登录成功。
	LoginResultSuccess int16 = 1
)

// SmsChallenge 验证码挑战。CodeHash 为 sha256(verificationID|code)，不保存明文。
// json tag 与 Redis 存储/原子消费脚本（Lua cjson）保持一致，改名需同步修改脚本。
type SmsChallenge struct {
	// VerificationID 挑战编号，登录时回传用于定位挑战。
	VerificationID string `json:"verification_id"`
	// Scene 验证码场景（LOGIN / CHANGE_MOBILE_OLD / CHANGE_MOBILE_NEW），跨场景不可复用。
	Scene string `json:"scene"`
	// Mobile 规范化后的手机号。
	Mobile string `json:"mobile"`
	// CodeHash 验证码哈希，仅用于定长比较。
	CodeHash string `json:"code_hash"`
	// ExpiresAt 过期时间，超过即作废。
	ExpiresAt time.Time `json:"expires_at"`
	// Attempts 已错误次数，达到 SmsMaxAttempts 时作废。
	Attempts int32 `json:"attempts"`
}

// SmsCodeStore 验证码短期存储（Redis 实现见 internal/data；Redis 不可用时退化为进程内存储）。
type SmsCodeStore interface {
	SaveChallenge(ctx context.Context, challenge *SmsChallenge, ttl time.Duration) error
	GetChallenge(ctx context.Context, verificationID string) (*SmsChallenge, error)
	// GetChallengeByMobile 按手机号取最近一次未消费挑战（登录接口 verification_id 缺省时使用）。
	GetChallengeByMobile(ctx context.Context, mobile string) (*SmsChallenge, error)
	IncrAttempt(ctx context.Context, verificationID string) (int32, error)
	DeleteChallenge(ctx context.Context, verificationID string) error
	// CompareAndDelete 原子校验并消费挑战：哈希一致时删除并返回 true。
	// 用于防止并发双击同一验证码签发两个令牌（校验与消费不可分成两步）。
	CompareAndDelete(ctx context.Context, verificationID, codeHash string) (bool, error)
	// AcquireResendLock 获取发送锁，false 表示该场景下该手机号处于冷却期需限流。
	// 冷却按「场景 + 手机号」隔离：登录刚发过码，不应阻塞紧接着的换绑原号验证。
	AcquireResendLock(ctx context.Context, scene, mobile string, ttl time.Duration) (bool, error)
	// ReleaseResendLock 释放发送锁（短信通道异常时回滚冷却期，避免用户白等 60 秒）。
	ReleaseResendLock(ctx context.Context, scene, mobile string) error
}

// SmsSender 短信通道抽象，由外部供应商实现；本项目提供日志实现，不打印验证码。
type SmsSender interface {
	SendLoginCode(ctx context.Context, mobile, code string) error
}

// TokenIssuer 令牌签发与校验抽象。
// 说明：按《前端接口文档》§7.1「令牌结构待认证系统确定」，本模块先用 JWT 实现，
// 模块6统一认证设施落地后替换实现即可，业务代码不变。
type TokenIssuer interface {
	Issue(ctx context.Context, p *Passenger) (token string, expiresInSeconds int32, err error)
	Parse(ctx context.Context, token string) (passengerID int64, err error)
}

// PassengerAuthRecord 登录审计记录（passenger_auth_records）。
type PassengerAuthRecord struct {
	PassengerID   *int64
	Mobile        string
	AuthType      int16
	LoginResult   int16
	FailureReason string
	DeviceID      string
	DeviceName    string
	DeviceModel   string
	ClientIP      string
	LoginLocation string
	UserAgent     string
	CreatedAt     time.Time
}

// PassengerAuthRecordRepo 登录审计数据访问接口。
type PassengerAuthRecordRepo interface {
	Create(ctx context.Context, record *PassengerAuthRecord) error
}

// LoginInput 登录入参（乘客ID由服务端解析，不接受客户端传入）。
// 设备相关字段由接口层从 User-Agent / 请求头解析后传入，业务层不直接解析 HTTP。
type LoginInput struct {
	Mobile         string
	SmsCode        string
	VerificationID string
	DeviceID       string
	DeviceName     string
	DeviceModel    string
	OSName         string
	OSVersion      string
	AppVersion     string
	ClientIP       string
	LoginLocation  string
	UserAgent      string
}

// LoginResult 登录结果。
type LoginResult struct {
	Passenger   *Passenger
	AccessToken string
	ExpiresIn   int32
	IsNewUser   bool
}

// SmsCodeTicket 验证码发送结果（不含真实验证码）。
type SmsCodeTicket struct {
	VerificationID     string
	ExpiresInSeconds   int32
	ResendAfterSeconds int32
	MaskedMobile       string
}

// AuthUsecase 登录/注册用例。
type AuthUsecase struct {
	passengerRepo  PassengerRepo
	authRecordRepo PassengerAuthRecordRepo
	codeStore      SmsCodeStore
	smsSender      SmsSender
	tokenIssuer    TokenIssuer
	log            *log.Helper
}

// NewAuthUsecase 构造函数（wire 注入）。
func NewAuthUsecase(
	passengerRepo PassengerRepo,
	authRecordRepo PassengerAuthRecordRepo,
	codeStore SmsCodeStore,
	smsSender SmsSender,
	tokenIssuer TokenIssuer,
	logger log.Logger,
) *AuthUsecase {
	return &AuthUsecase{
		passengerRepo:  passengerRepo,
		authRecordRepo: authRecordRepo,
		codeStore:      codeStore,
		smsSender:      smsSender,
		tokenIssuer:    tokenIssuer,
		log:            log.NewHelper(log.With(logger, "module", "biz/auth")),
	}
}

// SendLoginCode 发送登录验证码（兼容既有调用：等价于不带乘客身份的 LOGIN 场景）。
func (uc *AuthUsecase) SendLoginCode(ctx context.Context, mobile, scene string) (*SmsCodeTicket, error) {
	return uc.SendSmsCode(ctx, 0, mobile, scene)
}

// SendSmsCode 发送指定场景的验证码。
// 换绑手机号场景必须携带乘客身份：原号需与账号一致，新号需未被其他账号占用。
func (uc *AuthUsecase) SendSmsCode(ctx context.Context, passengerID int64, mobile, scene string) (*SmsCodeTicket, error) {
	normalized, err := validate.Mobile(mobile)
	if err != nil {
		// 格式校验在 pkg/validate 完成，这里补上业务码。
		return nil, invalidArgument("%s", err.Error())
	}
	if scene == "" {
		scene = SmsCodeSceneLogin
	}
	scene = strings.ToUpper(scene)
	if err := uc.checkSceneAllows(ctx, passengerID, normalized, scene); err != nil {
		return nil, err
	}

	// 后端限流：同一场景下同一手机号固定冷却期，前端倒计时不作为安全依据。
	acquired, err := uc.codeStore.AcquireResendLock(ctx, scene, normalized, SmsResendInterval)
	if err != nil {
		return nil, wrapInternal(err)
	}
	if !acquired {
		return nil, ErrSmsTooFrequent
	}

	code := randomDigits(6)
	verificationID := newVerificationID()
	challenge := &SmsChallenge{
		VerificationID: verificationID,
		Scene:          scene,
		Mobile:         normalized,
		CodeHash:       hashSmsCode(verificationID, code),
		ExpiresAt:      time.Now().Add(SmsCodeTTL),
	}
	if err := uc.codeStore.SaveChallenge(ctx, challenge, SmsCodeTTL); err != nil {
		return nil, wrapInternal(err)
	}
	if err := uc.smsSender.SendLoginCode(ctx, normalized, code); err != nil {
		// 发送失败立即作废挑战并释放冷却锁，避免用户拿到无效验证码还要等 60 秒。
		_ = uc.codeStore.DeleteChallenge(ctx, verificationID)
		_ = uc.codeStore.ReleaseResendLock(ctx, scene, normalized)
		return nil, wrapInternal(err)
	}

	uc.log.WithContext(ctx).Infof("登录验证码已发送 mobile=%s verification_id=%s", mask.Mobile(normalized), verificationID)
	return &SmsCodeTicket{
		VerificationID:     verificationID,
		ExpiresInSeconds:   int32(SmsCodeTTL / time.Second),
		ResendAfterSeconds: int32(SmsResendInterval / time.Second),
		MaskedMobile:       mask.Mobile(normalized),
	}, nil
}

// MobileLogin 验证码登录；手机号不存在时自动注册（事务内同时初始化钱包）。
// 验证码校验拆到 verifyLoginChallenge，保证本函数只做登录编排（agent/05 §4 函数 ≤80 行）。
func (uc *AuthUsecase) MobileLogin(ctx context.Context, in *LoginInput) (*LoginResult, error) {
	if in == nil {
		return nil, invalidArgument("请求参数为空")
	}
	mobile, err := validate.Mobile(in.Mobile)
	if err != nil {
		// 格式校验在 pkg/validate 完成，这里补上业务码。
		return nil, invalidArgument("%s", err.Error())
	}
	code := strings.TrimSpace(in.SmsCode)
	if len(code) != 6 || !isDigits(code) {
		return nil, invalidArgument("验证码格式不正确")
	}

	// 校验并一次性消费验证码挑战，失败时已写入登录审计。
	if err := uc.verifyLoginChallenge(ctx, mobile, code, in); err != nil {
		return nil, err
	}

	passenger, isNew, err := uc.findOrCreatePassenger(ctx, mobile)
	if err != nil {
		return nil, err
	}
	if passenger.AccountStatus != AccountStatusNormal {
		uc.recordAuth(ctx, &passenger.ID, mobile, in, LoginResultFailed, "账号状态异常")
		return nil, ErrAccountDisabled
	}

	token, expiresIn, err := uc.tokenIssuer.Issue(ctx, passenger)
	if err != nil {
		return nil, wrapInternal(err)
	}
	now := time.Now()
	if err := uc.passengerRepo.UpdateLastLogin(ctx, passenger.ID, now); err != nil {
		// 登录成功但审计字段写入失败不阻断登录，仅记录。
		uc.log.WithContext(ctx).Warnf("更新最后登录时间失败 passenger_id=%d err=%v", passenger.ID, err)
	}
	passenger.LastLoginAt = &now

	uc.recordAuth(ctx, &passenger.ID, mobile, in, LoginResultSuccess, "")
	uc.log.WithContext(ctx).Infof("乘客登录成功 passenger_id=%d new=%t", passenger.ID, isNew)

	return &LoginResult{Passenger: passenger, AccessToken: token, ExpiresIn: expiresIn, IsNewUser: isNew}, nil
}

// verifyLoginChallenge 校验登录场景验证码并一次性消费；失败分支写登录审计。
func (uc *AuthUsecase) verifyLoginChallenge(ctx context.Context, mobile, code string, in *LoginInput) error {
	if err := uc.consumeChallenge(ctx, SmsCodeSceneLogin, mobile, code, in.VerificationID); err != nil {
		uc.recordAuth(ctx, nil, mobile, in, LoginResultFailed, challengeFailureReason(err))
		return err
	}
	return nil
}

// consumeChallenge 校验指定场景的验证码并原子消费。
// 场景、手机号、有效期、错误次数逐项校验；任何不匹配都按「验证码失效」处理，不泄露挑战归属。
func (uc *AuthUsecase) consumeChallenge(ctx context.Context, scene, mobile, code, verificationID string) error {
	challenge, err := uc.loadChallenge(ctx, mobile, verificationID)
	if err != nil {
		return err
	}
	if challenge == nil {
		return ErrSmsCodeExpired
	}
	// 跨场景、跨手机号复用一律按失效处理（例如拿换绑验证码去登录）。
	if challenge.Scene != scene || challenge.Mobile != mobile {
		return ErrSmsCodeExpired
	}
	if time.Now().After(challenge.ExpiresAt) {
		_ = uc.codeStore.DeleteChallenge(ctx, challenge.VerificationID)
		return ErrSmsCodeExpired
	}
	if challenge.Attempts >= SmsMaxAttempts {
		_ = uc.codeStore.DeleteChallenge(ctx, challenge.VerificationID)
		return ErrSmsAttemptLimited
	}

	// 定长比较，避免按比较耗时推测验证码。
	expected := []byte(challenge.CodeHash)
	actual := []byte(hashSmsCode(challenge.VerificationID, code))
	if subtle.ConstantTimeCompare(expected, actual) != 1 {
		attempts, incErr := uc.codeStore.IncrAttempt(ctx, challenge.VerificationID)
		if incErr != nil {
			uc.log.WithContext(ctx).Warnf("验证码错误次数累加失败 verification_id=%s err=%v", challenge.VerificationID, incErr)
		}
		if attempts >= SmsMaxAttempts {
			_ = uc.codeStore.DeleteChallenge(ctx, challenge.VerificationID)
			return ErrSmsAttemptLimited
		}
		return ErrSmsCodeIncorrect
	}

	// 原子消费：校验哈希与删除在存储层一次完成，避免并发双击同一验证码签发两个令牌。
	consumed, err := uc.codeStore.CompareAndDelete(ctx, challenge.VerificationID, challenge.CodeHash)
	if err != nil {
		return wrapInternal(err)
	}
	if !consumed {
		return ErrSmsCodeExpired
	}
	return nil
}

// challengeFailureReason 把验证码错误映射成审计用的简短原因（不记录验证码本身）。
func challengeFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrSmsAttemptLimited):
		return "验证码错误次数过多"
	case errors.Is(err, ErrSmsCodeIncorrect):
		return "验证码不正确"
	default:
		return "验证码已失效"
	}
}

// checkSceneAllows 校验场景是否允许该手机号发送验证码。
func (uc *AuthUsecase) checkSceneAllows(ctx context.Context, passengerID int64, mobile, scene string) error {
	switch scene {
	case SmsCodeSceneLogin:
		return nil
	case SmsCodeSceneChangeOldMobile, SmsCodeSceneChangeNewMobile:
		if passengerID <= 0 {
			return ErrUnauthorized
		}
		current, err := uc.passengerRepo.GetByID(ctx, passengerID)
		if err != nil {
			return err
		}
		if current.AccountStatus != AccountStatusNormal {
			return ErrAccountDisabled
		}
		if scene == SmsCodeSceneChangeOldMobile {
			if current.Mobile != mobile {
				return businessConflict("原手机号与当前账号不一致")
			}
			return nil
		}
		// CHANGE_MOBILE_NEW：新号不能等于原号，也不能已被他人绑定。
		if current.Mobile == mobile {
			return invalidArgument("新手机号不能与当前手机号相同")
		}
		existing, err := uc.passengerRepo.GetByMobile(ctx, mobile)
		switch {
		case err == nil && existing != nil && existing.ID != passengerID:
			return businessConflict("该手机号已被其他账号使用")
		case err != nil && !errors.Is(err, ErrPassengerNotFound):
			return err
		}
		return nil
	default:
		return invalidArgument("不支持的验证码场景：%s", scene)
	}
}

// ChangeMobileInput 换绑手机号入参。
type ChangeMobileInput struct {
	NewMobile         string
	OldSmsCode        string
	OldVerificationID string
	NewSmsCode        string
	NewVerificationID string
	DeviceID          string
	DeviceName        string
	DeviceModel       string
	ClientIP          string
	LoginLocation     string
	UserAgent         string
}

// ChangeMobile 更换绑定手机号：原号与新号都必须通过各自场景的验证码。
// 只校验新号就能换绑的话，任何人拿到他人手机验证码即可抢占账号，因此强制双验证。
func (uc *AuthUsecase) ChangeMobile(ctx context.Context, passengerID int64, in *ChangeMobileInput) (*Passenger, error) {
	if passengerID <= 0 {
		return nil, ErrUnauthorized
	}
	if in == nil {
		return nil, invalidArgument("请求参数为空")
	}
	current, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	if current.AccountStatus != AccountStatusNormal {
		return nil, ErrAccountDisabled
	}

	newMobile, err := validate.Mobile(in.NewMobile)
	if err != nil {
		return nil, invalidArgument("%s", err.Error())
	}
	if newMobile == current.Mobile {
		return nil, invalidArgument("新手机号不能与当前手机号相同")
	}
	if !isDigits(strings.TrimSpace(in.OldSmsCode)) || !isDigits(strings.TrimSpace(in.NewSmsCode)) {
		return nil, invalidArgument("验证码格式不正确")
	}

	// 1) 校验原手机号归属
	if err := uc.consumeChallenge(ctx, SmsCodeSceneChangeOldMobile, current.Mobile,
		strings.TrimSpace(in.OldSmsCode), in.OldVerificationID); err != nil {
		uc.recordMobileChange(ctx, &passengerID, current.Mobile, in, LoginResultFailed, challengeFailureReason(err))
		return nil, err
	}
	// 2) 校验新手机号归属
	if err := uc.consumeChallenge(ctx, SmsCodeSceneChangeNewMobile, newMobile,
		strings.TrimSpace(in.NewSmsCode), in.NewVerificationID); err != nil {
		uc.recordMobileChange(ctx, &passengerID, current.Mobile, in, LoginResultFailed, challengeFailureReason(err))
		return nil, err
	}
	// 3) 新号不能绑定在他人账号上（并发场景由 mobile 唯一索引兜底）
	if existing, err := uc.passengerRepo.GetByMobile(ctx, newMobile); err == nil && existing != nil && existing.ID != passengerID {
		return nil, businessConflict("该手机号已被其他账号使用")
	} else if err != nil && !errors.Is(err, ErrPassengerNotFound) {
		return nil, err
	}
	// 4) 落库
	if err := uc.passengerRepo.UpdateMobile(ctx, passengerID, newMobile); err != nil {
		if errors.Is(err, ErrBusinessConflict) {
			return nil, businessConflict("该手机号已被其他账号使用")
		}
		return nil, err
	}

	updated, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	uc.recordMobileChange(ctx, &passengerID, current.Mobile, in, LoginResultSuccess, "")
	uc.log.WithContext(ctx).Infof("乘客更换绑定手机号成功 passenger_id=%d old=%s new=%s",
		passengerID, mask.Mobile(current.Mobile), mask.Mobile(newMobile))
	return updated, nil
}

// recordMobileChange 写换绑手机号审计（沿用登录审计表，auth_type=2）。
func (uc *AuthUsecase) recordMobileChange(ctx context.Context, passengerID *int64, mobile string, in *ChangeMobileInput, result int16, reason string) {
	record := &PassengerAuthRecord{
		PassengerID:   passengerID,
		Mobile:        mask.Mobile(mobile),
		AuthType:      AuthTypeMobileChange,
		LoginResult:   result,
		FailureReason: reason,
		CreatedAt:     time.Now(),
	}
	if in != nil {
		record.DeviceID = in.DeviceID
		record.DeviceName = in.DeviceName
		record.DeviceModel = in.DeviceModel
		record.ClientIP = in.ClientIP
		record.LoginLocation = in.LoginLocation
		record.UserAgent = truncate(in.UserAgent, 255)
	}
	if err := uc.authRecordRepo.Create(ctx, record); err != nil {
		uc.log.WithContext(ctx).Warnf("写换绑手机号审计失败 err=%v", err)
	}
}

// Authenticate 校验访问令牌并返回乘客ID，供本模块各查询/写接口做用户隔离。
// 说明：这里校验后回查账号状态，保证冻结账号的存量令牌立即失效；
// 模块6统一鉴权中间件落地后可下沉到中间件，本方法作为兜底保留。
func (uc *AuthUsecase) Authenticate(ctx context.Context, token string) (int64, error) {
	token = trimBearer(token)
	if token == "" {
		return 0, ErrUnauthorized
	}
	passengerID, err := uc.tokenIssuer.Parse(ctx, token)
	if err != nil || passengerID <= 0 {
		return 0, ErrUnauthorized
	}
	passenger, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		if errors.Is(err, ErrPassengerNotFound) {
			return 0, ErrUnauthorized
		}
		return 0, err
	}
	if passenger.AccountStatus != AccountStatusNormal {
		return 0, ErrAccountDisabled
	}
	return passengerID, nil
}

// loadChallenge 优先按挑战编号查询，未传时按手机号取最近挑战（兼容前端只传手机号的场景）。
func (uc *AuthUsecase) loadChallenge(ctx context.Context, mobile, verificationID string) (*SmsChallenge, error) {
	verificationID = strings.TrimSpace(verificationID)
	if verificationID != "" {
		challenge, err := uc.codeStore.GetChallenge(ctx, verificationID)
		if err != nil {
			return nil, wrapInternal(err)
		}
		return challenge, nil
	}
	challenge, err := uc.codeStore.GetChallengeByMobile(ctx, mobile)
	if err != nil {
		return nil, wrapInternal(err)
	}
	return challenge, nil
}

// findOrCreatePassenger 按手机号查找乘客，不存在则注册并初始化钱包。
func (uc *AuthUsecase) findOrCreatePassenger(ctx context.Context, mobile string) (*Passenger, bool, error) {
	passenger, err := uc.passengerRepo.GetByMobile(ctx, mobile)
	if err == nil && passenger != nil {
		return passenger, false, nil
	}
	if err != nil && !errors.Is(err, ErrPassengerNotFound) {
		return nil, false, err
	}

	now := time.Now()
	created := &Passenger{
		PassengerNo:    newPassengerNo(),
		Mobile:         mobile,
		Nickname:       defaultNickname(mobile),
		RealNameStatus: RealNameStatusNone,
		AccountStatus:  AccountStatusNormal,
		PointBalance:   0,
		Preferences:    DefaultPreferences(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := uc.passengerRepo.Create(ctx, created); err != nil {
		// 并发注册：同一手机号同时登录时唯一键冲突，此时以先落库的账号为准直接登录。
		if errors.Is(err, ErrBusinessConflict) {
			if existing, getErr := uc.passengerRepo.GetByMobile(ctx, mobile); getErr == nil && existing != nil {
				return existing, false, nil
			}
		}
		return nil, false, err
	}
	return created, true, nil
}

// trimBearer 去掉 Authorization 的 Bearer 前缀，大小写不敏感（bearer/Bearer 都要兼容）。
func trimBearer(authorization string) string {
	value := strings.TrimSpace(authorization)
	if len(value) >= 7 && strings.EqualFold(value[:7], "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}

// recordAuth 写登录审计；失败不阻断主流程（审计降级）。
func (uc *AuthUsecase) recordAuth(ctx context.Context, passengerID *int64, mobile string, in *LoginInput, result int16, reason string) {
	record := &PassengerAuthRecord{
		PassengerID:   passengerID,
		Mobile:        mask.Mobile(mobile),
		AuthType:      AuthTypeMobileSms,
		LoginResult:   result,
		FailureReason: reason,
		CreatedAt:     time.Now(),
	}
	if in != nil {
		record.DeviceID = in.DeviceID
		record.DeviceName = in.DeviceName
		record.DeviceModel = in.DeviceModel
		record.ClientIP = in.ClientIP
		record.LoginLocation = in.LoginLocation
		record.UserAgent = truncate(in.UserAgent, 255)
	}
	if err := uc.authRecordRepo.Create(ctx, record); err != nil {
		uc.log.WithContext(ctx).Warnf("写登录审计失败 err=%v", err)
	}
}

// hashSmsCode 验证码哈希：sha256(verification_id|code)，仅存哈希不存明文（AGENT.md §10）。
func hashSmsCode(verificationID, code string) string {
	sum := sha256.Sum256([]byte(verificationID + "|" + code))
	return hex.EncodeToString(sum[:])
}

// newVerificationID 生成验证码挑战编号。
func newVerificationID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("verify_%d", time.Now().UnixNano())
	}
	return "verify_" + hex.EncodeToString(buf)
}

// newPassengerNo 生成乘客编号：P + 纳秒时间戳片段 + 3 位随机数，唯一约束兜底。
func newPassengerNo() string {
	return fmt.Sprintf("P%d%03d", time.Now().UnixMilli(), mrand.Intn(1000))
}

// randomDigits 生成指定长度数字串（加密随机源）。
func randomDigits(n int) string {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return strings.Repeat("0", n)
	}
	return fmt.Sprintf("%0*s", n, v.String())
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func defaultNickname(mobile string) string {
	if len(mobile) >= 4 {
		return "乘客" + mobile[len(mobile)-4:]
	}
	return "乘客"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
