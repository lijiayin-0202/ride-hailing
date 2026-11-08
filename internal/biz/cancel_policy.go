package biz

import "fmt"

// 取消费用规则（金额为分）。规则口径依据《萝卜快跑开发任务》4.5「取消记录、取消费用、状态结果」，
// 具体金额在评审前保持集中定义，禁止散落到 handler/data 层。
const (
	CancelRulePendingFree        = "PENDING_FREE"
	CancelRulePickingUpFee       = "PICKING_UP_FEE"
	CancelRuleWaitingBoardingFee = "WAITING_BOARDING_FEE"

	cancelFeePickingUpCents       int64 = 500
	cancelFeeWaitingBoardingCents int64 = 1000
)

var cancelRuleDescription = map[string]string{
	CancelRulePendingFree:        "派单前取消不收取费用",
	CancelRulePickingUpFee:       "车辆接驾中取消收取违约金 5.00 元",
	CancelRuleWaitingBoardingFee: "车辆已到达后取消收取违约金 10.00 元",
}

// CancelFee 返回当前状态取消订单应收取的费用与规则编码。
// 费用不会超过订单应付金额；行程开始后（4 及以上）不允许取消。
func CancelFee(status OrderStatus, payableCents int64) (int64, string, error) {
	var fee int64
	var rule string
	switch status {
	case OrderStatusPendingDispatch, OrderStatusDispatching:
		fee, rule = 0, CancelRulePendingFree
	case OrderStatusPickingUp:
		fee, rule = cancelFeePickingUpCents, CancelRulePickingUpFee
	case OrderStatusWaitingBoarding:
		fee, rule = cancelFeeWaitingBoardingCents, CancelRuleWaitingBoardingFee
	default:
		return 0, "", fmt.Errorf("%w: order status %d cannot be cancelled", ErrConflict, status)
	}
	if payableCents > 0 && fee > payableCents {
		fee = payableCents
	}
	return fee, rule, nil
}

// CancelRuleText 返回规则的中文说明，用于接口回显。
func CancelRuleText(rule string) string {
	if text, ok := cancelRuleDescription[rule]; ok {
		return text
	}
	return ""
}

// RefundableCents 计算已支付订单取消后退还的金额。
func RefundableCents(paidCents, feeCents int64) int64 {
	if paidCents <= feeCents {
		return 0
	}
	return paidCents - feeCents
}
