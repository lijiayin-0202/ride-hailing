package biz

import (
	"context"
	"time"
)

const defaultDispatchStrategy = "first-eligible-v1"
const defaultDispatchAverageSpeedKMH = 30.0

// PendingDispatchAttempt 是原子创建待确认派单所需的事实集合。
type PendingDispatchAttempt struct {
	Snapshot        OrderDispatchSnapshot
	Candidate       CandidateVehicle
	Record          DispatchRecord
	Deadline        time.Time
	Strategy        string
	AverageSpeedKMH float64
}

// AssignDispatchResult 是 HTTP 自动派单入口需要返回的领域结果。
type AssignDispatchResult struct {
	Record               DispatchRecord
	ConfirmationDeadline time.Time
	ReusedActiveAttempt  bool
}

// PendingDispatchAttemptRepository 定义活跃派单读取和原子新建端口。
type PendingDispatchAttemptRepository interface {
	FindActive(context.Context, int64) (DispatchRecord, bool, error)
	CreatePending(context.Context, PendingDispatchAttempt) (AssignDispatchResult, error)
}

// AutoDispatchUsecase 编排模块 2 查询后的自动派单入口。
type AutoDispatchUsecase struct {
	orders   OrderDispatchQueryGateway
	attempts PendingDispatchAttemptRepository
	clock    Clock
}

// NewAutoDispatchUsecase 创建自动派单用例。
func NewAutoDispatchUsecase(
	orders OrderDispatchQueryGateway,
	attempts PendingDispatchAttemptRepository,
	clock Clock,
) *AutoDispatchUsecase {
	return &AutoDispatchUsecase{orders: orders, attempts: attempts, clock: clock}
}

// Assign 查询订单后复用已有活跃派单，后续候选创建由原子仓储接入。
func (usecase *AutoDispatchUsecase) Assign(ctx context.Context, orderID int64) (AssignDispatchResult, error) {
	// 订单可调度性以模块 2 的实时快照为准，模块 3 不复制或猜测订单状态。
	snapshot, err := usecase.orders.GetDispatchableOrder(ctx, orderID)
	if err != nil {
		return AssignDispatchResult{}, err
	}
	if snapshot.OrderID != orderID || snapshot.ServiceAreaID == 0 || snapshot.Cancelled || !snapshot.Schedulable {
		return AssignDispatchResult{}, ErrOrderNotSchedulable
	}
	// 在创建新记录前复用活跃尝试，使重复 HTTP 请求具备业务幂等性。
	active, found, err := usecase.attempts.FindActive(ctx, orderID)
	if err != nil {
		return AssignDispatchResult{}, err
	}
	if found {
		return reusedDispatchResult(active, usecase.clock.Now()), nil
	}
	return usecase.attempts.CreatePending(ctx, PendingDispatchAttempt{
		Snapshot: snapshot, Deadline: usecase.clock.Now().Add(confirmationTimeout),
		Strategy: defaultDispatchStrategy, AverageSpeedKMH: defaultDispatchAverageSpeedKMH,
	})
}

func reusedDispatchResult(record DispatchRecord, now time.Time) AssignDispatchResult {
	deadline := now.Add(confirmationTimeout)
	if record.DispatchedAt != nil {
		deadline = record.DispatchedAt.Add(confirmationTimeout)
	}
	return AssignDispatchResult{Record: record, ConfirmationDeadline: deadline, ReusedActiveAttempt: true}
}
