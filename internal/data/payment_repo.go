package data

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// HandlePaymentCallback 收敛渠道回调。幂等口径：
//   - 同一支付单重复收到 SUCCESS 回调时不再记账，返回 duplicated=true；
//   - 回调金额必须与服务端支付单金额一致，避免前端/渠道篡改金额。
func (r *rideRepo) HandlePaymentCallback(ctx context.Context, callback *biz.PaymentCallback) (*biz.PaymentCallbackResult, error) {
	var row paymentRow
	err := r.data.gormDB.WithContext(ctx).Where("payment_no = ?", callback.PaymentNo).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, biz.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	payment := paymentFromRow(row)
	result := &biz.PaymentCallbackResult{Payment: payment, OrderID: row.OrderID}

	if callback.Result == "SUCCESS" {
		if callback.AmountCents != int64(row.Amount) {
			return nil, biz.ErrConflict
		}
		if row.PaymentStatus == biz.PaymentStatusPaid {
			result.Duplicated = true
			return result, nil
		}
		now := callback.OccurredAt
		if now.IsZero() {
			now = time.Now()
		}
		if err := r.data.gormDB.WithContext(ctx).Table("payments").
			Where("payment_no = ? AND payment_status <> ?", callback.PaymentNo, biz.PaymentStatusPaid).
			Updates(map[string]any{
				"payment_status":    biz.PaymentStatusPaid,
				"provider_trade_no": callback.ChannelTradeNo,
				"paid_at":           now,
				"updated_at":        now,
			}).Error; err != nil {
			return nil, err
		}
		payment.Status = biz.PaymentStatusPaid
		payment.ChannelTradeNo = callback.ChannelTradeNo
		payment.PaidAt = &now
		return result, nil
	}

	if row.PaymentStatus == biz.PaymentStatusFailed {
		result.Duplicated = true
		return result, nil
	}
	now := time.Now()
	if err := r.data.gormDB.WithContext(ctx).Table("payments").
		Where("payment_no = ?", callback.PaymentNo).
		Updates(map[string]any{
			"payment_status": biz.PaymentStatusFailed,
			"failure_code":   "CHANNEL_FAILED",
			"updated_at":     now,
		}).Error; err != nil {
		return nil, err
	}
	payment.Status = biz.PaymentStatusFailed
	return result, nil
}

// createRefund 写退款流水并按金额收敛支付单状态，供取消订单复用。
func (r *rideRepo) createRefund(tx *gorm.DB, refund *biz.Refund, paidCents int64) error {
	if refund.AmountCents <= 0 {
		return nil
	}
	now := time.Now()
	if err := tx.Table("refunds").Create(map[string]any{
		"refund_no": refund.RefundNo, "payment_id": refund.PaymentID,
		"order_id": refund.OrderID, "passenger_id": refund.PassengerID,
		"refund_amount": cents(refund.AmountCents), "refund_reason": refund.Reason,
		"refund_status": int16(2), "idempotency_key": refund.RefundNo,
		"requested_by": int16(3), "requested_at": now, "refunded_at": now, "updated_at": now,
	}).Error; err != nil {
		return err
	}
	status := biz.PaymentStatusPartiallyRefunded
	if refund.AmountCents >= paidCents {
		status = biz.PaymentStatusRefunded
	}
	return tx.Table("payments").
		Where("order_id = ? AND payment_status IN ?", refund.OrderID,
			[]int16{biz.PaymentStatusPaid, biz.PaymentStatusPartiallyRefunded}).
		Updates(map[string]any{
			"payment_status":  status,
			"refunded_amount": gorm.Expr("refunded_amount + ?", cents(refund.AmountCents)),
			"updated_at":      now,
		}).Error
}
