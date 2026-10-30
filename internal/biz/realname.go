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
	Status         int16
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
	log           *log.Helper
}

// NewRealNameUsecase 构造函数（wire 注入）。
// 盐值由 conf 层统一读取环境变量后注入（.agent/04 §2.1），本层不感知配置来源。
//
// 安全策略（fail-closed）：盐值缺失时直接拒绝启动。
// 原因：身份证号取值空间很小（地区码+生日+顺序码），若退化为写死的公共盐，
// 加盐 SHA-256 用 GPU 可被穷举反查，等价于明文入库。
func NewRealNameUsecase(repo RealNameRepo, passengerRepo PassengerRepo, idCardSalt RealNameSalt, logger log.Logger) (*RealNameUsecase, error) {
	salt := strings.TrimSpace(string(idCardSalt))
	if salt == "" {
		return nil, fmt.Errorf("未配置实名认证证件哈希盐值（环境变量 %s）：为避免身份证号被反查，拒绝启动", conf.EnvIDCardSalt)
	}
	return &RealNameUsecase{
		repo:          repo,
		passengerRepo: passengerRepo,
		idCardSalt:    salt,
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

	record, err := uc.repo.Submit(ctx, &RealNameRecord{
		PassengerID:    passengerID,
		RealNameMasked: mask.RealName(name),
		IDCardMasked:   mask.IDCard(idCard),
		IDCardHash:     hash,
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
