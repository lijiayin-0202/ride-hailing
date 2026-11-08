package biz

import (
	"errors"
	"testing"
)

func TestCancelFeeRules(t *testing.T) {
	tests := []struct {
		name     string
		status   OrderStatus
		payable  int64
		wantFee  int64
		wantRule string
		wantErr  error
	}{
		{name: "pending dispatch free", status: OrderStatusPendingDispatch, payable: 2000, wantFee: 0, wantRule: CancelRulePendingFree},
		{name: "dispatching free", status: OrderStatusDispatching, payable: 2000, wantFee: 0, wantRule: CancelRulePendingFree},
		{name: "picking up charged", status: OrderStatusPickingUp, payable: 2000, wantFee: 500, wantRule: CancelRulePickingUpFee},
		{name: "waiting boarding charged", status: OrderStatusWaitingBoarding, payable: 2000, wantFee: 1000, wantRule: CancelRuleWaitingBoardingFee},
		{name: "fee capped by payable", status: OrderStatusWaitingBoarding, payable: 300, wantFee: 300, wantRule: CancelRuleWaitingBoardingFee},
		{name: "in trip rejected", status: OrderStatusInTrip, payable: 2000, wantErr: ErrConflict},
		{name: "completed rejected", status: OrderStatusCompleted, payable: 2000, wantErr: ErrConflict},
		{name: "exception closed rejected", status: OrderStatusExceptionClosed, payable: 2000, wantErr: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fee, rule, err := CancelFee(test.status, test.payable)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("expected error %v, actual %v", test.wantErr, err)
			}
			if fee != test.wantFee || rule != test.wantRule {
				t.Fatalf("expected %d/%s, actual %d/%s", test.wantFee, test.wantRule, fee, rule)
			}
			if test.wantErr == nil && CancelRuleText(rule) == "" {
				t.Fatalf("rule %s has no description", rule)
			}
		})
	}
}

func TestRefundableCents(t *testing.T) {
	tests := []struct{ paid, fee, want int64 }{
		{paid: 0, fee: 0, want: 0},
		{paid: 0, fee: 500, want: 0},
		{paid: 2000, fee: 500, want: 1500},
		{paid: 500, fee: 500, want: 0},
	}
	for _, test := range tests {
		if actual := RefundableCents(test.paid, test.fee); actual != test.want {
			t.Fatalf("paid %d fee %d: expected %d, actual %d", test.paid, test.fee, test.want, actual)
		}
	}
}
