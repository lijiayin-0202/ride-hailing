package biz

import (
	"context"
	"errors"
	"time"
)

var ErrOrderNotFound = errors.New("订单不存在")
var ErrOrderGatewayUnavailable = errors.New("订单服务不可用")

// OrderDispatchSnapshot 是模块 2 返回的派单决策所需订单事实快照。
type OrderDispatchSnapshot struct {
	OrderID       int64
	ServiceAreaID int64
	Pickup        DispatchLocation
	Dropoff       DispatchLocation
	OrderStatus   string
	Cancelled     bool
	Schedulable   bool
	// ReservedAt 保留预约单的原始事实，供后续策略升级使用；当前不改变本轮派单资格判断。
	ReservedAt time.Time
}

// OrderDispatchEventType 表示模块 3 同步给模块 2 的订单派单阶段。
type OrderDispatchEventType int16

const (
	OrderDispatchEventAttemptPending OrderDispatchEventType = iota + 1
	OrderDispatchEventAssigned
	OrderDispatchEventRetrying
	OrderDispatchEventExhausted
	OrderDispatchEventRejected
	OrderDispatchEventTimedOut
)

// OrderDispatchEvent 是可靠状态同步消息的领域载荷。
type OrderDispatchEvent struct {
	// DispatchNo 同时作为跨服务幂等键，重试投递同一事件不会重复推进订单状态。
	DispatchNo string
	OrderID    int64
	DispatchID int64
	VehicleID  int64
	Type       OrderDispatchEventType
	Reason     string
	OccurredAt time.Time
}

// OrderDispatchQueryGateway 定义模块 2 的订单派单快照查询端口。
type OrderDispatchQueryGateway interface {
	GetDispatchableOrder(context.Context, int64) (OrderDispatchSnapshot, error)
}

// OrderDispatchSyncGateway 定义模块 2 的订单派单状态同步端口。
type OrderDispatchSyncGateway interface {
	Sync(context.Context, OrderDispatchEvent) (bool, error)
}
