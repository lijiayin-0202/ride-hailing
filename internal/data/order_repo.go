package data

import (
	"context"
	"errors"
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
	err = r.data.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

func (r *rideRepo) createOrderTransaction(tx *gorm.DB, order *biz.Order, row *OrderRow) error {
	var activeCount int64
	if err := tx.Model(&OrderRow{}).
		Where("passenger_id = ? AND status IN (0, 1, 2, 3)", order.PassengerID).
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
		"estimated_duration_min": (row.EstimatedDurationS + 59) / 60,
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
			"estimated_amount": cents(item.AmountCents), "created_at": time.Now(),
		}
		if err := tx.Table("order_fee_items").Create(fee).Error; err != nil {
			return err
		}
	}
	return tx.Table("trip_status_logs").Create(map[string]any{
		"order_id": row.ID, "current_status": int16(order.Status), "change_source": int16(1),
		"description": "订单创建",
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
	err := r.data.db.WithContext(ctx).
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
	err = r.data.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		PassengerCouponID *int64
		BoardingCodeHash  string
	}
	if err := tx.Table("trip_orders").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id, status, passenger_coupon_id, boarding_code_hash").
		Where("id = ? AND passenger_id = ?", command.OrderID, command.PassengerID).
		Take(&locked).Error; err != nil {
		return err
	}
	if biz.OrderStatus(locked.Status) != command.CurrentStatus {
		return biz.ErrConflict
	}
	if command.Type == "UNLOCK_AND_START" && hashSecret(command.BoardingCode) != locked.BoardingCodeHash {
		return biz.ErrConflict
	}
	if err := tx.Create(row).Error; err != nil || command.ResultStatus == command.CurrentStatus {
		return err
	}
	lockedRow := &OrderRow{ID: locked.ID, OrderStatus: locked.Status, PassengerCouponID: locked.PassengerCouponID}
	return r.transitionOrder(tx, lockedRow, command)
}

func (r *rideRepo) transitionOrder(tx *gorm.DB, locked *OrderRow, command *biz.OrderCommand) error {
	updates := map[string]any{"status": int16(command.ResultStatus), "updated_at": time.Now()}
	if command.Type == "CANCEL" {
		updates["cancelled_at"] = time.Now()
		if locked.PassengerCouponID != nil {
			if err := tx.Table("passenger_coupons").Where("id = ? AND status = 1", *locked.PassengerCouponID).Update("status", 0).Error; err != nil {
				return err
			}
		}
	} else if command.Type == "UNLOCK_AND_START" {
		updates["started_at"] = time.Now()
	} else if command.Type == "MARK_ARRIVED" {
		updates["arrived_at"] = time.Now()
	} else if command.Type == "START_TRIP" {
		updates["started_at"] = time.Now()
	} else if command.Type == "COMPLETE_TRIP" {
		updates["completed_at"] = time.Now()
	}
	result := tx.Model(&OrderRow{}).
		Where("id = ? AND passenger_id = ? AND status = ?", command.OrderID, command.PassengerID, int16(command.CurrentStatus)).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return biz.ErrConflict
	}
	return tx.Table("trip_status_logs").Create(map[string]any{
		"order_id": command.OrderID, "previous_status": int16(command.CurrentStatus),
		"current_status": int16(command.ResultStatus), "change_source": int16(1),
		"description": command.Type,
	}).Error
}
