package data

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func (r *rideRepo) CreateOrder(ctx context.Context, order *biz.Order) (*biz.Order, bool, error) {
	existing, err := r.findOrderByIdempotency(ctx, order.PassengerID, order.IdempotencyKey)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, biz.ErrNotFound) {
		return nil, false, err
	}
	row, err := r.newOrderRow(order)
	if err != nil {
		return nil, false, err
	}
	err = r.data.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.createOrderTransaction(tx, order, row)
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		existing, findErr := r.findOrderByIdempotency(ctx, order.PassengerID, order.IdempotencyKey)
		return existing, true, findErr
	}
	if err != nil {
		return nil, false, err
	}
	order.ID, order.OrderNo = row.ID, row.OrderNo
	order.BoardingCodeSuffix = row.BoardingCodeSuffix
	return order, false, nil
}

func (r *rideRepo) FindOrderByIdempotency(ctx context.Context, passengerID int64, idempotencyKey string) (*biz.Order, error) {
	return r.findOrderByIdempotency(ctx, passengerID, idempotencyKey)
}

func (r *rideRepo) createOrderTransaction(tx *gorm.DB, order *biz.Order, row *OrderRow) error {
	var activeCount int64
	// 活动订单判定使用状态机口径：0 待派车 ~ 4 行程中。
	if err := tx.Model(&OrderRow{}).
		Where("passenger_id = ? AND status BETWEEN ? AND ?", order.PassengerID,
			int16(biz.OrderStatusPendingDispatch), int16(biz.OrderStatusInTrip)).
		Count(&activeCount).Error; err != nil {
		return err
	}
	if activeCount > 0 {
		return biz.ErrConflict
	}
	if order.PassengerCouponID > 0 {
		result := tx.Table("passenger_coupons").
			Where("id = ? AND passenger_id = ? AND status = 0", order.PassengerCouponID, order.PassengerID).
			Update("status", 1)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return biz.ErrConflict
		}
	}
	result := tx.Table("trip_orders").Create(map[string]any{
		"order_no": row.OrderNo, "passenger_id": row.PassengerID, "service_area_id": row.ServiceAreaID,
		"pickup_station_id": row.PickupStationID, "dropoff_station_id": row.DropoffStationID,
		"pricing_rule_id": row.PricingRuleID, "passenger_coupon_id": row.PassengerCouponID,
		"status": row.OrderStatus, "pickup_snapshot": row.PickupSnapshot, "dropoff_snapshot": row.DropoffSnapshot,
		"estimated_distance_km":  float64(row.EstimatedDistanceM) / 1000,
		"estimated_distance_m":   row.EstimatedDistanceM,
		"estimated_duration_min": (row.EstimatedDurationS + 59) / 60,
		"estimated_duration_s":   row.EstimatedDurationS,
		"ride_mode":              row.RideMode,
		"estimated_amount":       row.EstimatedAmount, "original_amount": row.OriginalAmount,
		"discount_amount": row.DiscountAmount, "payable_amount": row.PayableAmount,
		"quote_id": row.QuoteID, "service_type_id": row.ServiceTypeID, "idempotency_key": row.IdempotencyKey,
		"order_type": orderType(row.RideMode), "reserved_at": row.ScheduledAt,
		"passenger_count": row.PassengerCount, "contact_phone_ciphertext": row.ContactPhoneCiphertext,
		"contact_phone_masked": row.ContactPhoneMasked, "user_agreement_version": row.UserAgreementVersion,
		"pricing_rule_version": row.PricingRuleVersion, "boarding_code_hash": row.BoardingCodeHash,
		"boarding_code_suffix": row.BoardingCodeSuffix, "created_at": row.CreatedAt, "updated_at": row.CreatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if err := tx.Table("trip_orders").Select("id").Where("order_no = ?", row.OrderNo).Take(row).Error; err != nil {
		return err
	}
	for _, item := range order.Fare.Items {
		feeType := int16(7)
		if item.Type == "BASE_FARE" {
			feeType = 1
		} else if item.Type == "DISTANCE_FARE" {
			feeType = 2
		} else if item.Type == "COUPON" {
			feeType = 5
		}
		fee := map[string]any{
			"order_id": row.ID, "fee_type": feeType, "fee_name": item.Name,
			"amount": cents(item.AmountCents), "item_type": item.Type, "item_name": item.Name,
			"estimated_amount": cents(item.AmountCents), "final_amount": cents(item.AmountCents),
			"calculation_detail": json.RawMessage(`{}`), "created_at": time.Now(),
		}
		if err := tx.Table("order_fee_items").Create(fee).Error; err != nil {
			return err
		}
	}
	return tx.Table("trip_status_logs").Create(map[string]any{
		"order_id": row.ID, "current_status": int16(biz.OrderStatusPendingDispatch),
		"change_source": int16(1), "description": "创建订单",
		"reason_code": "CREATE_ORDER",
	}).Error
}

func orderType(rideMode string) int16 {
	if rideMode == "SCHEDULED" {
		return 2
	}
	return 1
}

func (r *rideRepo) GetOrder(ctx context.Context, passengerID, orderID int64) (*biz.Order, error) {
	row, err := r.loadOrder(ctx, "o.id = ? AND o.passenger_id = ?", orderID, passengerID)
	if err != nil {
		return nil, err
	}
	return orderFromDetail(row), nil
}

func (r *rideRepo) ExecuteCommand(ctx context.Context, command *biz.OrderCommand) (*biz.OrderCommand, error) {
	var existing commandRow
	err := r.data.gormDB.WithContext(ctx).
		Where("passenger_id = ? AND idempotency_key = ?", command.PassengerID, command.IdempotencyKey).
		First(&existing).Error
	if err == nil {
		current, loadErr := r.GetOrder(ctx, command.PassengerID, command.OrderID)
		if loadErr != nil {
			return nil, loadErr
		}
		result := commandFromRow(existing)
		result.ResultStatus = current.Status
		return result, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row := commandToRow(command)
	err = r.data.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.executeCommandTransaction(tx, command, row)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, biz.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	command.ID, command.Accepted = row.ID, true
	return command, nil
}

func (r *rideRepo) executeCommandTransaction(tx *gorm.DB, command *biz.OrderCommand, row *commandRow) error {
	var locked struct {
		ID                int64
		Status            int16
		PassengerID       int64
		PassengerCouponID *int64
		BoardingCodeHash  string
	}
	if err := tx.Table("trip_orders").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id, status, passenger_id, passenger_coupon_id, boarding_code_hash").
		Where("id = ? AND passenger_id = ?", command.OrderID, command.PassengerID).
		Take(&locked).Error; err != nil {
		return err
	}
	if biz.OrderStatus(locked.Status) != command.CurrentStatus {
		return biz.ErrConflict
	}
	if command.Type == biz.CommandUnlockAndStart && hashSecret(command.BoardingCode) != locked.BoardingCodeHash {
		return biz.ErrConflict
	}
	if err := tx.Create(row).Error; err != nil {
		return err
	}
	if command.ResultStatus == command.CurrentStatus {
		return nil
	}
	lockedRow := &OrderRow{ID: locked.ID, OrderStatus: locked.Status, PassengerCouponID: locked.PassengerCouponID}
	return r.transitionOrder(tx, lockedRow, command)
}

func (r *rideRepo) transitionOrder(tx *gorm.DB, locked *OrderRow, command *biz.OrderCommand) error {
	now := time.Now()
	updates := map[string]any{"status": int16(command.ResultStatus), "updated_at": now}
	switch command.Type {
	case biz.CommandUnlockAndStart:
		updates["boarded_at"], updates["started_at"] = now, now
	case biz.CommandStartTrip:
		updates["boarded_at"], updates["started_at"] = now, now
	case biz.CommandDispatch:
		updates["dispatched_at"] = now
	case biz.CommandDispatchAccept:
		updates["dispatched_at"] = now
		if command.VehicleID > 0 {
			updates["vehicle_id"] = command.VehicleID
		}
	case biz.CommandMarkArrived:
		updates["arrived_at"] = now
	case biz.CommandCompleteTrip:
		updates["completed_at"] = now
	}
	result := tx.Model(&OrderRow{}).
		Where("id = ? AND passenger_id = ? AND status = ?", command.OrderID, command.PassengerID, int16(command.CurrentStatus)).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return biz.ErrConflict
	}
	if err := r.recordDispatch(tx, command, now); err != nil {
		return err
	}
	return tx.Table("trip_status_logs").Create(map[string]any{
		"order_id": command.OrderID, "previous_status": int16(command.CurrentStatus),
		"current_status": int16(command.ResultStatus),
		"change_source":  changeSource(command.Type),
		"description":    command.Type, "reason_code": command.Type,
	}).Error
}

// recordDispatch 维护 dispatch_records，让 live-view 的派单摘要与实际派车一致。
func (r *rideRepo) recordDispatch(tx *gorm.DB, command *biz.OrderCommand, now time.Time) error {
	switch command.Type {
	case biz.CommandDispatch:
		return tx.Table("dispatch_records").Create(map[string]any{
			"dispatch_no": newPublicID("dsp", now), "order_id": command.OrderID,
			"dispatch_strategy": "ADMIN_MANUAL", "status": int16(1),
			"dispatched_at": now, "created_at": now,
		}).Error
	case biz.CommandDispatchAccept:
		result := tx.Table("dispatch_records").Where("order_id = ? AND status = 1", command.OrderID).
			Updates(map[string]any{"status": int16(2), "vehicle_id": command.VehicleID, "responded_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		return tx.Table("dispatch_records").Create(map[string]any{
			"dispatch_no": newPublicID("dsp", now), "order_id": command.OrderID,
			"vehicle_id": command.VehicleID, "dispatch_strategy": "ADMIN_MANUAL",
			"status": int16(2), "dispatched_at": now, "responded_at": now, "created_at": now,
		}).Error
	}
	return nil
}

// changeSource 对应 trip_status_logs.change_source：1 乘客 / 2 车辆 / 3 调度 / 4 后台 / 5 系统。
func changeSource(commandType string) int16 {
	switch commandType {
	case biz.CommandUnlockAndStart, biz.CommandRequestPullOver:
		return 1
	case biz.CommandMarkArrived, biz.CommandStartTrip, biz.CommandCompleteTrip:
		return 2
	case biz.CommandDispatch, biz.CommandDispatchAccept:
		return 3
	case biz.CommandCloseException:
		return 4
	}
	return 5
}

// CancelOrder 落库取消结果：订单状态、优惠券释放、状态流水、取消记录与退款流水在同一事务内完成。
func (r *rideRepo) CancelOrder(ctx context.Context, cancellation *biz.Cancellation) (*biz.Cancellation, error) {
	var alreadyCancelled bool
	var releaseCoupon *int64
	var paidCents int64
	err := r.data.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked struct {
			ID                int64
			Status            int16
			PassengerCouponID *int64
		}
		if err := tx.Table("trip_orders").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, status, passenger_coupon_id").
			Where("id = ? AND passenger_id = ?", cancellation.OrderID, cancellation.PassengerID).
			Take(&locked).Error; err != nil {
			return err
		}
		current := biz.OrderStatus(locked.Status)
		if current == biz.OrderStatusCancelled {
			alreadyCancelled = true
			return nil
		}
		if !biz.CanTransit(current, biz.OrderStatusCancelled) {
			return biz.ErrConflict
		}
		releaseCoupon = locked.PassengerCouponID

		now := cancellation.CancelledAt
		if now.IsZero() {
			now = time.Now()
		}
		if err := tx.Table("trip_orders").
			Where("id = ? AND passenger_id = ? AND status = ?", cancellation.OrderID, cancellation.PassengerID, int16(current)).
			Updates(map[string]any{"status": int16(biz.OrderStatusCancelled), "cancelled_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if releaseCoupon != nil {
			if err := tx.Table("passenger_coupons").
				Where("id = ? AND status = 1", *releaseCoupon).Update("status", 0).Error; err != nil {
				return err
			}
		}
		if err := tx.Table("trip_status_logs").Create(map[string]any{
			"order_id": cancellation.OrderID, "previous_status": int16(current),
			"current_status": int16(biz.OrderStatusCancelled), "change_source": int16(1),
			"description": "CANCEL:" + cancellation.ReasonCode,
			"reason_code": cancellation.ReasonCode,
		}).Error; err != nil {
			return err
		}
		// order_cancellations 的真实结构：cancelled_by(1 乘客) + cancellation_fee + reason_detail。
		if err := tx.Table("order_cancellations").Create(map[string]any{
			"order_id": cancellation.OrderID, "cancelled_by": int16(1),
			"reason_code":      cancellation.ReasonCode,
			"reason_detail":    cancellation.RuleCode + " " + biz.CancelRuleText(cancellation.RuleCode),
			"cancellation_fee": cents(cancellation.FeeCents),
			"created_at":       now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Table("payments").Select("COALESCE(SUM(amount),0)").
			Where("order_id = ? AND payment_status IN ?", cancellation.OrderID,
				[]int16{biz.PaymentStatusPaid, biz.PaymentStatusPartiallyRefunded}).
			Scan(&paidCents).Error; err != nil {
			return err
		}
		if cancellation.RefundCents > 0 && paidCents > 0 {
			var payment paymentRow
			if err := tx.Where("order_id = ? AND payment_status = ?", cancellation.OrderID, biz.PaymentStatusPaid).
				Order("id DESC").First(&payment).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if payment.ID > 0 {
				refund := &biz.Refund{
					RefundNo: newPublicID("rf", now), PaymentID: payment.ID,
					OrderID: cancellation.OrderID, PassengerID: cancellation.PassengerID,
					AmountCents: cancellation.RefundCents, Reason: "CANCEL_" + cancellation.RuleCode,
				}
				if err := r.createRefund(tx, refund, paidCents); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if alreadyCancelled {
		return r.loadCancellation(ctx, cancellation.OrderID, cancellation.PassengerID)
	}
	cancellation.Status = biz.OrderStatusCancelled
	cancellation.PaidCents = paidCents
	return cancellation, nil
}

func (r *rideRepo) loadCancellation(ctx context.Context, orderID, passengerID int64) (*biz.Cancellation, error) {
	// order_cancellations 的 order_id 有唯一约束，且没有 passenger_id 字段。
	rows, err := r.queryRowMaps(ctx,
		`SELECT * FROM order_cancellations WHERE order_id = ? ORDER BY id DESC LIMIT 1`, orderID)
	if err != nil {
		if isOptionalRelationError(err) {
			return nil, biz.ErrNotFound
		}
		return nil, err
	}
	if len(rows) == 0 {
		return nil, biz.ErrNotFound
	}
	row := rows[0]
	ruleCode := strings.SplitN(mapString(row, "reason_detail"), " ", 2)[0]
	cancellation := &biz.Cancellation{
		OrderID:     mapInt64(row, "order_id"),
		PassengerID: mapInt64(row, "passenger_id"),
		ReasonCode:  mapString(row, "reason_code", "reason"),
		RuleCode:    ruleCode,
		FeeCents:    mapCents(row, "cancellation_fee", "fee_amount"),
		Status:      biz.OrderStatusCancelled,
	}
	if cancelledAt := mapTime(row, "created_at", "cancelled_at"); cancelledAt != nil {
		cancellation.CancelledAt = *cancelledAt
	}
	for _, refundRow := range r.allRows(ctx, `SELECT * FROM refunds WHERE order_id = ? AND refund_status = ?`, orderID, 2) {
		cancellation.RefundCents += mapCents(refundRow, "refund_amount")
	}
	return cancellation, nil
}
