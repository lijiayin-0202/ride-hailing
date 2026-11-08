// 模块1 用户账号与个人中心（成员A）
// 实名认证领域模型与用例。
// 数据表：passenger_real_name_records（passengers.real_name_status 同步更新）
//
// 数据安全（AGENT.md §10）：
//  1. 姓名、身份证号**不明文入库**，只保存掩码（展示用）与加盐 SHA-256 哈希（去重/防重绑用）；
//  2. 日志只输出掩码，禁止打印证件原文；
//  3. 盐值由 conf 层注入（.agent/04 §2.1：业务层禁止裸读环境变量），禁止写死在业务代码里。
package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/pkg/mask"
	"github.com/lijayin-0202/radish-ride-service/pkg/securebox"
	"github.com/lijayin-0202/radish-ride-service/pkg/validate"
)

// realNameResubmitInterval 同一乘客重复提交实名的最小间隔，防止反复覆盖刷库。
const realNameResubmitInterval = 60 * time.Second

// RealNameRecord 实名认证记录（passenger_real_name_records）。
type RealNameRecord struct {
	ID             int64
	PassengerID    int64
	RealNameMasked string
	IDCardMasked   string
	IDCardHash     string
	// RealNameCipher 姓名 AES-256-GCM 密文（v1:<base64>），仅供服务端核验/审计解密。
	RealNameCipher string
	// IDCardCipher 身份证号 AES-256-GCM 密文，接口与日志都不输出。
	IDCardCipher string
	Status       int16
	RejectReason   string
	SubmittedAt    time.Time
	VerifiedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RealNameInfo 实名认证查询结果：乘客维度状态 + 最近一次记录（可能为空）。
type RealNameInfo struct {
	PassengerStatus int16
	Record          *RealNameRecord
}

// RealNameRepo 实名认证数据访问接口（实现在 internal/data）。
type RealNameRepo interface {
	// GetByPassenger 查询最近一次认证记录；从未提交返回 (nil, nil)。
	GetByPassenger(ctx context.Context, passengerID int64) (*RealNameRecord, error)
	// Submit 覆盖式写入认证记录，并在同一事务内把 passengers.real_name_status 置为「认证中」。
	Submit(ctx context.Context, record *RealNameRecord) (*RealNameRecord, error)
	// IDCardBound 判断该身份证哈希是否已被其他乘客占用（status 为认证中/已认证）。
	IDCardBound(ctx context.Context, idCardHash string, excludePassengerID int64) (bool, error)
}

// RealNameSalt 证件哈希盐值。
// 使用独立类型而非裸 string，避免与其它字符串依赖在 wire 注入时混淆。
type RealNameSalt string

// ProvideRealNameSalt 从 conf 读取盐值并提供给 wire（配置读取仍收敛在 internal/conf）。
func ProvideRealNameSalt() RealNameSalt { return RealNameSalt(conf.IDCardSalt()) }

// RealNameUsecase 实名认证用例。
type RealNameUsecase struct {
	repo          RealNameRepo
	passengerRepo PassengerRepo
	idCardSalt    string
	// box 敏感字段加解密器；密钥缺失时构造失败，服务拒绝启动。
	box *securebox.Box
	log *log.Helper
}

// ProvideRealNameBox 从 conf 读取 AES-256-GCM 密钥并构造加密器（wire 注入）。
//
// fail-closed：密钥缺失或长度不合法时直接拒绝启动。
// 原因：密文列若写入空值，等于把「已加密」变成事实上的不加密，
// 且历史数据无法区分，事后无法补救。
func ProvideRealNameBox() (*securebox.Box, error) {
	key := conf.DataKey()
	if key == "" {
		// 报错要可执行：直接告诉使用者怎么生成与注入，避免只看到 "拒绝启动" 无从下手。
		return nil, fmt.Errorf("未配置敏感字段加密密钥（环境变量 %s）：拒绝启动。"+
			"最简做法：运行 scripts/run.ps1（自动生成并写入 .env 后加载）；"+
			"手工方式见 README「初始化与启动」，密钥必须是 32 字节 base64 且长期固定", conf.EnvDataKey)
	}
	return securebox.NewFromBase64(key)
}

// NewRealNameUsecase 构造函数（wire 注入）。
// 盐值由 conf 层统一读取环境变量后注入（.agent/04 §2.1），本层不感知配置来源。
//
// 安全策略（fail-closed）：盐值缺失时直接拒绝启动。
// 原因：身份证号取值空间很小（地区码+生日+顺序码），若退化为写死的公共盐，
// 加盐 SHA-256 用 GPU 可被穷举反查，等价于明文入库。
func NewRealNameUsecase(repo RealNameRepo, passengerRepo PassengerRepo, idCardSalt RealNameSalt, box *securebox.Box, logger log.Logger) (*RealNameUsecase, error) {
	if box == nil {
		return nil, fmt.Errorf("敏感字段加密器未初始化（环境变量 %s）：拒绝启动", conf.EnvDataKey)
	}
	salt := strings.TrimSpace(string(idCardSalt))
	if salt == "" {
		return nil, fmt.Errorf("未配置实名认证证件哈希盐值（环境变量 %s）：为避免身份证号被反查，拒绝启动", conf.EnvIDCardSalt)
	}
	return &RealNameUsecase{
		repo:          repo,
		passengerRepo: passengerRepo,
		idCardSalt:    salt,
		box:           box,
		log:           log.NewHelper(log.With(logger, "module", "biz/realname")),
	}, nil
}

// Get 查询实名认证状态。
// 乘客不存在返回 ErrPassengerNotFound；从未提交时 Record 为 nil。
func (uc *RealNameUsecase) Get(ctx context.Context, passengerID int64) (*RealNameInfo, error) {
	passenger, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	record, err := uc.repo.GetByPassenger(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return &RealNameInfo{PassengerStatus: passenger.RealNameStatus, Record: record}, nil
}

// RealNameCheck 内部实名校验结果：同时返回实名状态与账号状态。
// 下单前判定必须两者都满足，冻结/注销账号即使已实名也不允许继续下单。
type RealNameCheck struct {
	// RealNameStatus 0 未认证 1 认证中 2 已认证 3 已驳回。
	RealNameStatus int16
	// AccountStatus 0 冻结 1 正常 2 注销。
	AccountStatus int16
}

// CheckStatus 供内部服务（订单/行程模块）在下单前校验实名与账号状态。
//
// 该能力只通过 gRPC 暴露，不提供 HTTP 路由：乘客端无法用他人身份调用，
// 避免出现「查别人是否实名」的越权面。是否允许下单由调用方按
// 「RealNameStatus=2 且 AccountStatus=1」判断，本层不做业务决策。
func (uc *RealNameUsecase) CheckStatus(ctx context.Context, passengerID int64) (*RealNameCheck, error) {
	if passengerID <= 0 {
		return nil, invalidArgument("乘客ID不合法")
	}
	passenger, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return &RealNameCheck{
		RealNameStatus: passenger.RealNameStatus,
		AccountStatus:  passenger.AccountStatus,
	}, nil
}

// Submit 提交实名认证。
//
// 业务规则：
//  1. 已认证（status=2）不允许重复提交；
//  2. 认证中（1）与驳回（3）允许重新提交，覆盖原记录（便于更正资料后重新审核）；
//  3. 同一身份证号不允许绑定多个账号（认证中/已认证记录参与判重）；
//  4. 提交成功后 passengers.real_name_status 置为「认证中」，审核结果由后台/人工流转到 2 或 3。
func (uc *RealNameUsecase) Submit(ctx context.Context, passengerID int64, realName, idCardNo string) (*RealNameRecord, error) {
	passenger, err := uc.passengerRepo.GetByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	if passenger.RealNameStatus == RealNameStatusVerified {
		return nil, businessConflict("已完成实名认证，无需重复提交")
	}
	// 提交频控：认证中的记录 60 秒内不允许重复覆盖，避免被脚本刷库。
	if last, getErr := uc.repo.GetByPassenger(ctx, passengerID); getErr != nil {
		return nil, getErr
	} else if last != nil && last.Status == RealNameStatusPending &&
		time.Since(last.SubmittedAt) < realNameResubmitInterval {
		return nil, ErrRealNameTooFrequent
	}

	// 姓名与证件格式校验在 pkg/validate 完成（.agent/01 §3），这里补业务码。
	name, err := validate.RealName(realName)
	if err != nil {
		return nil, invalidArgument("%s", err.Error())
	}
	idCard, err := validate.IDCard(idCardNo)
	if err != nil {
		return nil, invalidArgument("%s", err.Error())
	}

	hash := uc.hashIDCard(idCard)
	bound, err := uc.repo.IDCardBound(ctx, hash, passengerID)
	if err != nil {
		return nil, err
	}
	if bound {
		// 对外统一模糊话术：不回显「已绑定其他账号」，避免被用来枚举某个证件是否注册过。
		// 真实原因只写服务端日志，供客服/安全排查。
		uc.log.WithContext(ctx).Warnf("实名认证证件已被占用 passenger_id=%d id_card=%s", passengerID, mask.IDCard(idCard))
		return nil, businessConflict("证件信息校验未通过，请核对后重新提交")
	}

	// AES-256-GCM 密文（UA-T0-09）：明文只在本次调用内存中存在，
	// 落库的是密文 + 掩码 + 哈希，日志只打掩码。
	nameCipher, err := uc.box.Encrypt(name)
	if err != nil {
		return nil, wrapInternal(err)
	}
	idCardCipher, err := uc.box.Encrypt(idCard)
	if err != nil {
		return nil, wrapInternal(err)
	}

	record, err := uc.repo.Submit(ctx, &RealNameRecord{
		PassengerID:    passengerID,
		RealNameMasked: mask.RealName(name),
		IDCardMasked:   mask.IDCard(idCard),
		IDCardHash:     hash,
		RealNameCipher: nameCipher,
		IDCardCipher:   idCardCipher,
		Status:         RealNameStatusPending,
	})
	if err != nil {
		return nil, err
	}
	uc.log.WithContext(ctx).Infof("实名认证已提交 passenger_id=%d id_card=%s", passengerID, record.IDCardMasked)
	return record, nil
}

// 姓名/证件的格式校验与脱敏已下沉到公共包（.agent/01 §3）：
//   - 校验：pkg/validate.RealName、pkg/validate.IDCard
//   - 脱敏：pkg/mask.RealName、pkg/mask.IDCard

// hashIDCard 身份证号加盐 SHA-256，用于唯一性判重，不可逆。
// 盐值由构造函数保证非空（fail-closed），这里不再提供兜底盐。
func hashIDCard(idCard, salt string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + ":" + idCard))
	return hex.EncodeToString(sum[:])
}

// hashIDCard 使用注入的盐值计算证件哈希。
func (uc *RealNameUsecase) hashIDCard(idCard string) string {
	return hashIDCard(idCard, uc.idCardSalt)
}
