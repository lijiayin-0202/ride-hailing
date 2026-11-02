// 模块1 用户账号与个人中心（成员A）
// 紧急联系人领域模型与用例。
// 数据表：passenger_emergency_contacts（一名乘客只保留一条，重复保存覆盖）。
//
// 数据安全（AGENT.md §10）：联系人手机号属于敏感数据，
// 库内保存规范化后的号码，接口只返回脱敏值；日志禁止打印明文。
package biz

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/pkg/validate"
)

// 紧急联系人字段长度约束，与 migration 中的 VARCHAR 长度对齐。
const (
	emergencyContactMinNameLen     = 2
	emergencyContactMaxNameLen     = 30
	emergencyContactMaxRelationLen = 20
)

// EmergencyContact 紧急联系人实体（passenger_emergency_contacts）。
type EmergencyContact struct {
	// ID 主键。
	ID int64
	// PassengerID 归属乘客；所有读写必须带上做数据隔离。
	PassengerID int64
	// ContactName 联系人姓名。
	ContactName string
	// ContactMobile 联系人手机号（明文仅用于库内保存与内部校验，对外必须脱敏）。
	ContactMobile string
	// Relation 与乘客的关系，可为空。
	Relation string
	// CreatedAt 首次维护时间。
	CreatedAt time.Time
	// UpdatedAt 最近一次覆盖时间。
	UpdatedAt time.Time
}

// EmergencyContactRepo 紧急联系人数据访问接口（实现在 internal/data）。
type EmergencyContactRepo interface {
	// Get 按 passenger_id 查询；未维护返回 (nil, nil)，不返回错误。
	Get(ctx context.Context, passengerID int64) (*EmergencyContact, error)
	// Save 按 passenger_id 覆盖写入（存在则更新，不存在则新增）。
	Save(ctx context.Context, contact *EmergencyContact) error
}

// EmergencyContactUsecase 紧急联系人文例。
type EmergencyContactUsecase struct {
	repo EmergencyContactRepo
	log  *log.Helper
}

// NewEmergencyContactUsecase 构造函数（wire 注入）。
func NewEmergencyContactUsecase(repo EmergencyContactRepo, logger log.Logger) *EmergencyContactUsecase {
	return &EmergencyContactUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "biz/emergency_contact")),
	}
}

// Get 查询当前乘客的紧急联系人；未维护时返回 (nil, nil)，由接口层转成 has_contact=false。
func (uc *EmergencyContactUsecase) Get(ctx context.Context, passengerID int64) (*EmergencyContact, error) {
	if passengerID <= 0 {
		return nil, invalidArgument("乘客身份不合法")
	}
	return uc.repo.Get(ctx, passengerID)
}

// Save 保存（新增或覆盖）紧急联系人。
//
// 校验口径：
//  1. 姓名 2-30 个字符，允许中文、字母、空格与间隔号，规则与实名姓名一致；
//  2. 手机号复用 pkg/validate 的大陆手机号校验（含 +86/空格/短横线清洗）；
//  3. 关系可空，最长 20 个字符。
func (uc *EmergencyContactUsecase) Save(ctx context.Context, passengerID int64, name, mobile, relation string) (*EmergencyContact, error) {
	if passengerID <= 0 {
		return nil, invalidArgument("乘客身份不合法")
	}

	contactName, err := normalizeContactName(name)
	if err != nil {
		return nil, invalidArgument("%s", err.Error())
	}
	contactMobile, err := validate.Mobile(mobile)
	if err != nil {
		return nil, invalidArgument("联系人手机号格式不正确")
	}
	contactRelation := strings.TrimSpace(relation)
	if utf8.RuneCountInString(contactRelation) > emergencyContactMaxRelationLen {
		return nil, invalidArgument("与联系人关系不能超过%d个字符", emergencyContactMaxRelationLen)
	}

	now := time.Now()
	contact := &EmergencyContact{
		PassengerID:   passengerID,
		ContactName:   contactName,
		ContactMobile: contactMobile,
		Relation:      contactRelation,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := uc.repo.Save(ctx, contact); err != nil {
		return nil, err
	}
	uc.log.WithContext(ctx).Infof("乘客维护紧急联系人 passenger_id=%d", passengerID)
	return contact, nil
}

// normalizeContactName 规范化并校验联系人姓名。
func normalizeContactName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", invalidArgument("联系人姓名不能为空")
	}
	if count := utf8.RuneCountInString(name); count < emergencyContactMinNameLen || count > emergencyContactMaxNameLen {
		return "", invalidArgument("联系人姓名长度需为%d-%d个字符", emergencyContactMinNameLen, emergencyContactMaxNameLen)
	}
	for _, r := range name {
		if r == ' ' || r == '·' || r == '•' {
			continue
		}
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) {
			continue
		}
		return "", invalidArgument("联系人姓名只能包含中文、字母、空格或间隔号")
	}
	return name, nil
}
