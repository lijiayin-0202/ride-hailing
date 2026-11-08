package biz

import "fmt"

// 订单命令类型。乘客端只允许 passengerCommands，其余为调度/车端/客服内部命令。
const (
	CommandDispatch        = "DISPATCH"          // 调度发起派单：0 → 1
	CommandDispatchAccept  = "DISPATCH_ACCEPT"   // 车辆确认接单：1 → 2
	CommandMarkArrived     = "MARK_ARRIVED"      // 车辆到达上车点：2 → 3
	CommandStartTrip       = "START_TRIP"        // 车端开始行程：3 → 4
	CommandUnlockAndStart  = "UNLOCK_AND_START"  // 乘客核验后开门出发：3 → 4
	CommandCompleteTrip    = "COMPLETE_TRIP"     // 行程结束：4 → 5
	CommandCancel          = "CANCEL"            // 取消订单：0/1/2/3 → 6
	CommandCloseException  = "CLOSE_EXCEPTION"   // 异常关闭：任意非终态 → 7
	CommandRequestPullOver = "REQUEST_PULL_OVER" // 请求靠边停车：原地
)

var commandTargetStatus = map[string]OrderStatus{
	CommandDispatch:        OrderStatusDispatching,
	CommandDispatchAccept:  OrderStatusPickingUp,
	CommandMarkArrived:     OrderStatusWaitingBoarding,
	CommandStartTrip:       OrderStatusInTrip,
	CommandUnlockAndStart:  OrderStatusInTrip,
	CommandCompleteTrip:    OrderStatusCompleted,
	CommandCancel:          OrderStatusCancelled,
	CommandCloseException:  OrderStatusExceptionClosed,
	CommandRequestPullOver: OrderStatusInTrip,
}

// passengerCommands 是乘客端可以自助触发的命令，其余命令必须由内部令牌放行。
var passengerCommands = map[string]struct{}{
	CommandUnlockAndStart:  {},
	CommandRequestPullOver: {},
}

// allowedTransitions 是唯一的状态迁移事实来源，越界迁移一律拒绝。
var allowedTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusPendingDispatch: {OrderStatusDispatching, OrderStatusCancelled, OrderStatusExceptionClosed},
	OrderStatusDispatching:     {OrderStatusPendingDispatch, OrderStatusPickingUp, OrderStatusCancelled, OrderStatusExceptionClosed},
	OrderStatusPickingUp:       {OrderStatusWaitingBoarding, OrderStatusCancelled, OrderStatusExceptionClosed},
	OrderStatusWaitingBoarding: {OrderStatusInTrip, OrderStatusCancelled, OrderStatusExceptionClosed},
	OrderStatusInTrip:          {OrderStatusCompleted, OrderStatusExceptionClosed},
	OrderStatusCompleted:       {},
	OrderStatusCancelled:       {},
	OrderStatusExceptionClosed: {},
}

// CanTransit 判断订单是否允许从 from 迁移到 to。
func CanTransit(from, to OrderStatus) bool {
	if from == to {
		return true
	}
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// Terminal 表示订单已进入终态，不再接受任何迁移。
func (s OrderStatus) Terminal() bool {
	return len(allowedTransitions[s]) == 0
}

// Active 表示订单仍占用乘客的活动订单名额。
func (s OrderStatus) Active() bool {
	return s <= OrderStatusInTrip
}

// IsPassengerCommand 判断命令是否允许乘客端直接调用。
func IsPassengerCommand(commandType string) bool {
	_, ok := passengerCommands[commandType]
	return ok
}

// IsKnownCommand 判断命令是否在冻结的命令枚举内。
func IsKnownCommand(commandType string) bool {
	_, ok := commandTargetStatus[commandType]
	return ok
}

// commandTarget 计算命令执行后的目标状态，非法迁移返回 ErrConflict。
func commandTarget(current OrderStatus, commandType string) (OrderStatus, error) {
	target, ok := commandTargetStatus[commandType]
	if !ok {
		return current, fmt.Errorf("%w: unsupported command %s", ErrInvalidArgument, commandType)
	}
	if commandType == CommandRequestPullOver {
		if current != OrderStatusInTrip {
			return current, fmt.Errorf("%w: order is not in trip", ErrConflict)
		}
		return current, nil
	}
	if target == current {
		return current, nil
	}
	if !CanTransit(current, target) {
		return current, fmt.Errorf("%w: illegal transition from %d to %d", ErrConflict, current, target)
	}
	return target, nil
}
