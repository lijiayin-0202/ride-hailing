// 模块1 用户账号与个人中心（成员A）
// 积分查询与每日签到。
// 数据表：point_transactions（积分流水）、sign_in_records（每日签到）、passengers.point_balance
package biz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// 积分流水来源（point_transactions.source_type）
const (
	PointSourceTrip     int32 = 1 // 完成行程
	PointSourceSignIn   int32 = 2 // 每日签到
	PointSourceExchange int32 = 3 // 兑换消耗
	PointSourceManual   int32 = 4 // 人工调整
)

// 签到奖励规则。
// 注意：数据字典未定义具体分值，这里按「每日 5 分、连续每满 7 天额外 15 分」实现，
// 属待产品确认项（已在交付说明的风险清单中列出），不写入订单金额等资金字段。
const (
	// SignInBasePoints 每日签到基础积分。
	SignInBasePoints int32 = 5
	// SignInCycleDays 连续签到奖励周期（每满 7 天加奖）。
	SignInCycleDays int32 = 7
	// SignInCycleBonusPoints 满周期额外奖励积分。
	SignInCycleBonusPoints int32 = 15
)

// bizLocation 业务时区：签到按自然日计算，统一使用北京时间。
var bizLocation = time.FixedZone("CST", 8*3600)

// StartOfDay 返回给定时刻在业务时区当天的 00:00:00。
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// PointTransaction 积分流水（point_transactions）。
type PointTransaction struct {
	// ID 流水主键。
	ID int64
	// TransactionNo 流水号（唯一），对账用。
	TransactionNo string
	// ChangeAmount 变动值：正为获得、负为消耗。
	ChangeAmount int32
	// BalanceAfter 变动后余额，便于回放核对。
	BalanceAfter int32
	// SourceType 来源：1 完成行程 / 2 每日签到 / 3 兑换消耗 / 4 人工调整。
	SourceType int32
	// RelatedOrderID 关联订单；nil 表示非行程来源。
	RelatedOrderID *int64
	// Remark 备注文案。
	Remark string
	// CreatedAt 发生时间。
	CreatedAt time.Time
}

// SignInRecord 签到记录（sign_in_records）。
type SignInRecord struct {
	// ID 记录主键。
	ID int64
	// PassengerID 乘客 ID。
	PassengerID int64
	// SignInDate 签到自然日（业务时区 00:00）。
	SignInDate time.Time
	// RewardPoints 当日获得积分。
	RewardPoints int32
	// ContinuousDays 截至当日的连续签到天数。
	ContinuousDays int32
	// CreatedAt 写入时间。
	CreatedAt time.Time
}

// SignInResult 签到结果。
type SignInResult struct {
	// SignInDate 签到日期。
	SignInDate time.Time
	// RewardPoints 本次获得积分（重复提交时为当日已有值）。
	RewardPoints int32
	// ContinuousDays 连续天数。
	ContinuousDays int32
	// PointBalance 签到后余额。
	PointBalance int32
	// AlreadySigned 当天是否已签到（幂等回放标记）。
	AlreadySigned bool
}

// PointListResult 积分流水分页结果。
type PointListResult struct {
	// PointBalance 当前积分余额。
	PointBalance int32
	// Items 当前页流水。
	Items []*PointTransaction
	// Total 流水总数。
	Total int64
	// Page 归一化后的页码。
	Page int32
	// PageSize 归一化后的每页条数。
	PageSize int32
}

// PointRepo 积分与签到数据访问接口。
type PointRepo interface {
	Balance(ctx context.Context, passengerID int64) (int32, error)
	ListTransactions(ctx context.Context, passengerID int64, offset, limit int32) ([]*PointTransaction, int64, error)
	// LastSignIn 查询 day 当天的签到记录，不存在返回 (nil, nil)。
	LastSignIn(ctx context.Context, passengerID int64, day time.Time) (*SignInRecord, error)
	// SignIn 事务内完成：写签到记录（passenger_id+sign_in_date 唯一）+ 加积分 + 写流水。
	// 当天已签到时返回已有记录并置 AlreadySigned=true，保证重复提交幂等。
	SignIn(ctx context.Context, passengerID int64, day time.Time, rewardPoints, continuousDays int32, remark string) (*SignInResult, error)
}

// PointUsecase 积分与签到用例。
type PointUsecase struct {
	repo PointRepo
	log  *log.Helper
}

// NewPointUsecase 构造函数（wire 注入）。
func NewPointUsecase(repo PointRepo, logger log.Logger) *PointUsecase {
	return &PointUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "biz/point")),
	}
}

// GetPoints 积分余额 + 流水分页。
func (uc *PointUsecase) GetPoints(ctx context.Context, passengerID int64, page, pageSize int32) (*PointListResult, error) {
	// 分页归一化，列表接口禁止返回全量。
	page, pageSize, offset := NormalizePage(page, pageSize)
	// 余额查询失败直接返回（乘客不存在时由数据层转成领域错误）。
	balance, err := uc.repo.Balance(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	items, total, err := uc.repo.ListTransactions(ctx, passengerID, offset, pageSize)
	if err != nil {
		return nil, err
	}
	return &PointListResult{PointBalance: balance, Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// SignIn 每日签到。重复提交返回当天已有结果，不重复加积分。
func (uc *PointUsecase) SignIn(ctx context.Context, passengerID int64) (*SignInResult, error) {
	// 业务时区自然日：今天的 00:00 与昨天同一时刻。
	today := StartOfDay(time.Now(), bizLocation)
	yesterday := today.AddDate(0, 0, -1)

	// 默认连续 1 天；昨天有签到记录则累加。
	continuous := int32(1)
	if prev, err := uc.repo.LastSignIn(ctx, passengerID, yesterday); err == nil && prev != nil {
		continuous = prev.ContinuousDays + 1
	} else if err != nil {
		// 查询失败不阻断签到，按断签处理并告警。
		uc.log.WithContext(ctx).Warnf("查询昨日签到失败 passenger_id=%d err=%v", passengerID, err)
	}

	// 奖励规则：基础分 + 每满 7 天的额外奖励。
	reward := SignInBasePoints
	if continuous%SignInCycleDays == 0 {
		reward += SignInCycleBonusPoints
	}

	result, err := uc.repo.SignIn(ctx, passengerID, today, reward, continuous, "每日签到")
	if err != nil {
		return nil, err
	}
	if !result.AlreadySigned {
		uc.log.WithContext(ctx).Infof("乘客签到成功 passenger_id=%d continuous=%d reward=%d", passengerID, result.ContinuousDays, result.RewardPoints)
	}
	return result, nil
}
