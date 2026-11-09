package data

import (
	"context"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// GetSettlement 聚合结算视图：订单、费用明细、支付单、已付/已退金额与评价状态。
func (r *rideRepo) GetSettlement(ctx context.Context, passengerID, orderID int64) (*biz.Settlement, error) {
	order, err := r.GetOrder(ctx, passengerID, orderID)
	if err != nil {
		return nil, err
	}
	settlement := &biz.Settlement{Order: order}

	for _, row := range r.allRows(ctx, `SELECT * FROM order_fee_items WHERE order_id = ? ORDER BY id`, orderID) {
		settlement.FeeItems = append(settlement.FeeItems, biz.SettlementFeeItem{
			Type:        mapString(row, "item_type", "fee_type"),
			Name:        mapString(row, "item_name", "fee_name"),
			AmountCents: mapCents(row, "final_amount", "amount", "estimated_amount"),
		})
	}
	if len(settlement.FeeItems) == 0 {
		for _, item := range order.Fare.Items {
			settlement.FeeItems = append(settlement.FeeItems, biz.SettlementFeeItem{
				Type: item.Type, Name: item.Name, AmountCents: item.AmountCents,
			})
		}
	}

	paymentRows := r.allRows(ctx, `SELECT * FROM payments WHERE order_id = ? ORDER BY id DESC`, orderID)
	for _, row := range paymentRows {
		status := int16(mapInt64(row, "payment_status", "status"))
		amount := mapCents(row, "amount")
		if status == biz.PaymentStatusPaid || status == biz.PaymentStatusPartiallyRefunded || status == biz.PaymentStatusRefunded {
			settlement.PaidCents += amount
		}
		if settlement.Payment == nil {
			settlement.Payment = &biz.SettlementPayment{
				PaymentID:   mapInt64(row, "id"),
				PaymentNo:   mapString(row, "payment_no"),
				Method:      paymentMethodText(int16(mapInt64(row, "payment_method", "method"))),
				Status:      paymentStatus(int64(status)),
				AmountCents: amount,
				PaidAt:      mapTime(row, "paid_at"),
			}
		}
	}

	// refund_status：2 表示退款成功，只有成功退款才计入已退金额。
	for _, row := range r.allRows(ctx, `SELECT * FROM refunds WHERE order_id = ? AND refund_status = ?`, orderID, 2) {
		settlement.RefundedCents += mapCents(row, "refund_amount", "amount")
	}

	if row := r.firstRow(ctx, `SELECT * FROM trip_reviews WHERE order_id = ? LIMIT 1`, orderID); row != nil {
		settlement.Review = &biz.SettlementReview{
			Submitted:   true,
			Rating:      int32(mapInt64(row, "overall_score", "rating", "score")),
			Tags:        decodeTags(mapBytes(row, "tags")),
			SubmittedAt: mapTime(row, "created_at"),
		}
		if settlement.Review.Rating == 0 {
			settlement.Review.Submitted = false
		}
	}
	return settlement, nil
}

// PaidAmount 返回订单已支付的金额（分），取消计费与退款以此为基数。
func (r *rideRepo) PaidAmount(ctx context.Context, orderID int64) (int64, error) {
	rows := r.allRows(ctx, `SELECT * FROM payments WHERE order_id = ?`, orderID)
	var paid int64
	for _, row := range rows {
		status := int16(mapInt64(row, "payment_status", "status"))
		if status == biz.PaymentStatusPaid || status == biz.PaymentStatusPartiallyRefunded || status == biz.PaymentStatusRefunded {
			paid += mapCents(row, "amount")
		}
	}
	return paid, nil
}

func paymentMethodText(code int16) string {
	return map[int16]string{1: "ALIPAY", 2: "WECHAT", 3: "BALANCE", 4: "MOCK"}[code]
}

func paymentStatus(code int64) string {
	return map[int64]string{0: "PENDING", 1: "PROCESSING", 2: "PAID", 3: "FAILED", 4: "CLOSED", 5: "PARTIAL_REFUNDED", 6: "REFUNDED"}[code]
}
