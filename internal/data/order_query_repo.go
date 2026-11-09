package data

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func (r *rideRepo) ListEvents(ctx context.Context, passengerID, orderID, afterID int64) ([]biz.OrderEvent, error) {
	var rows []struct {
		ID         int64
		EventType  string
		OccurredAt time.Time
		ToStatus   int16
		Longitude  float64
		Latitude   float64
		SpeedKMH   float64
	}
	err := r.data.gormDB.WithContext(ctx).Raw(`
		SELECT * FROM (
		  SELECT ((EXTRACT(EPOCH FROM e.occurred_at) * 1000000)::bigint * 100
		         + MOD(e.source_id, 50) * 2 + e.source_type)::bigint AS id,
		         e.event_type, e.occurred_at, e.to_status,
		         e.longitude, e.latitude, e.speed_kmh
		  FROM (
			SELECT 'ORDER_STATUS_CHANGED' AS event_type, sl.created_at AS occurred_at,
                   sl.current_status AS to_status, 0::numeric AS longitude, 0::numeric AS latitude,
			       0::numeric AS speed_kmh, sl.id AS source_id, 0 AS source_type
            FROM trip_status_logs sl
            JOIN trip_orders o ON o.id = sl.order_id
            WHERE sl.order_id = ? AND o.passenger_id = ?
            UNION ALL
			SELECT 'VEHICLE_LOCATION_UPDATED', ll.reported_at, o.status,
			       ll.longitude, ll.latitude, ll.speed_kmh, ll.id, 1
            FROM trip_location_logs ll
            JOIN trip_orders o ON o.id = ll.order_id
            WHERE ll.order_id = ? AND o.passenger_id = ?
		  ) e
		) events
		WHERE events.id > ?
		ORDER BY events.id
		LIMIT 100`, orderID, passengerID, orderID, passengerID, afterID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]biz.OrderEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, biz.OrderEvent{
			ID: row.ID, EventType: row.EventType, OccurredAt: row.OccurredAt,
			Status: biz.OrderStatus(row.ToStatus), Longitude: row.Longitude,
			Latitude: row.Latitude, SpeedKMH: row.SpeedKMH,
		})
	}
	return result, nil
}

func (r *rideRepo) CreatePayment(ctx context.Context, payment *biz.Payment) (*biz.Payment, bool, error) {
	var row paymentRow
	err := r.data.gormDB.WithContext(ctx).Where("passenger_id = ? AND idempotency_key = ?", payment.PassengerID, payment.IdempotencyKey).First(&row).Error
	if err == nil {
		return paymentFromRow(row), true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	row = paymentRow{}
	result := r.data.gormDB.WithContext(ctx).Table("payments").Create(map[string]any{
		"payment_no": payment.PaymentNo, "order_id": payment.OrderID,
		"passenger_id": payment.PassengerID, "payment_method": payment.Method,
		"amount": cents(payment.Amount), "payment_status": biz.PaymentStatusPending,
		"idempotency_key": payment.IdempotencyKey, "created_at": time.Now(), "updated_at": time.Now(),
	})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if err := r.data.gormDB.WithContext(ctx).Table("payments").Where("payment_no = ?", payment.PaymentNo).Take(&row).Error; err != nil {
		return nil, false, err
	}
	return paymentFromRow(row), false, nil
}

func (r *rideRepo) CreateReview(ctx context.Context, review *biz.Review) (*biz.Review, bool, error) {
	var row reviewRow
	err := r.data.gormDB.WithContext(ctx).Where("order_id = ?", review.OrderID).First(&row).Error
	if err == nil {
		return reviewFromRow(row), true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	result := r.data.gormDB.WithContext(ctx).Table("trip_reviews").Create(map[string]any{
		"order_id": review.OrderID, "passenger_id": review.PassengerID,
		"overall_score": review.Rating, "review_content": review.Comment,
		"is_anonymous": review.Anonymous, "review_status": 1,
		"tags": encodeTags(review.Tags), "created_at": review.CreatedAt, "updated_at": review.CreatedAt,
	})
	if result.Error != nil {
		// trip_reviews.order_id 上有唯一约束时，并发重复评价按幂等冲突处理。
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
			var duplicated reviewRow
			if err := r.data.gormDB.WithContext(ctx).Where("order_id = ?", review.OrderID).Take(&duplicated).Error; err == nil {
				return reviewFromRow(duplicated), true, nil
			}
		}
		return nil, false, result.Error
	}
	if err := r.data.gormDB.WithContext(ctx).Table("trip_reviews").Where("order_id = ?", review.OrderID).Take(&row).Error; err != nil {
		return nil, false, err
	}
	return reviewFromRow(row), false, nil
}

func (r *rideRepo) ListHistory(ctx context.Context, passengerID int64, cursor biz.HistoryCursor, pageSize int, statusGroup string) (*biz.HistoryPage, error) {
	query := r.orderDetailsQuery(ctx).Where("o.passenger_id = ?", passengerID)
	if !cursor.CreatedAt.IsZero() {
		query = query.Where("(o.created_at, o.id) < (?, ?)", cursor.CreatedAt, cursor.ID)
	}
	switch statusGroup {
	case "COMPLETED":
		query = query.Where("o.status = ?", int16(biz.OrderStatusCompleted))
	case "CANCELLED":
		query = query.Where("o.status IN (?, ?)", int16(biz.OrderStatusCancelled), int16(biz.OrderStatusExceptionClosed))
	}
	var rows []orderDetailRow
	if err := query.Order("o.created_at DESC, o.id DESC").Limit(pageSize + 1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	page := &biz.HistoryPage{HasMore: len(rows) > pageSize}
	if page.HasMore {
		rows = rows[:pageSize]
	}
	for _, row := range rows {
		page.Items = append(page.Items, orderFromDetail(row))
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		page.Next = &biz.HistoryCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}
