package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func (r *rideRepo) newOrderRow(order *biz.Order) (*OrderRow, error) {
	orderNo, err := newOrderNo(order.RequestedAt)
	if err != nil {
		return nil, err
	}
	boardingCode, err := newBoardingCode()
	if err != nil {
		return nil, err
	}
	ciphertext, err := encryptPhone(r.data.phoneKey, order.ContactPhone)
	if err != nil {
		return nil, err
	}
	pickupStationID, err := r.stationID(order.ServiceAreaID, order.Origin.PlaceID)
	if err != nil {
		return nil, err
	}
	dropoffStationID, err := r.stationID(order.ServiceAreaID, order.Destination.PlaceID)
	if err != nil {
		return nil, err
	}
	pickupSnapshot, _ := json.Marshal(order.Origin)
	dropoffSnapshot, _ := json.Marshal(order.Destination)
	row := &OrderRow{
		OrderNo: orderNo, PassengerID: order.PassengerID, ServiceAreaID: order.ServiceAreaID,
		OrderStatus:     int16(order.Status),
		PickupStationID: pickupStationID, DropoffStationID: dropoffStationID,
		PickupSnapshot: pickupSnapshot, DropoffSnapshot: dropoffSnapshot,
		PricingRuleID: &order.ServiceType.Rule.ID,
		PickupName:    placeName(order.Origin), PickupAddress: order.Origin.Address,
		PickupLongitude: order.Origin.Longitude, PickupLatitude: order.Origin.Latitude,
		DestinationName: placeName(order.Destination), DestinationAddress: order.Destination.Address,
		DestinationLongitude: order.Destination.Longitude, DestinationLatitude: order.Destination.Latitude,
		EstimatedDistanceM: order.DistanceMeters, EstimatedDurationS: order.EstimatedDurationSeconds,
		EstimatedAmount: cents(order.Fare.PayableCents), OriginalAmount: cents(order.Fare.OriginalCents),
		DiscountAmount: cents(order.Fare.DiscountCents), PayableAmount: cents(order.Fare.PayableCents),
		RequestedAt: order.RequestedAt, QuoteID: order.QuoteID, ServiceTypeID: order.ServiceType.ID,
		IdempotencyKey: order.IdempotencyKey, RideMode: order.RideMode,
		ScheduledAt: order.ScheduledAt, PassengerCount: order.PassengerCount,
		ContactPhoneCiphertext: ciphertext, ContactPhoneMasked: maskPhone(order.ContactPhone),
		UserAgreementVersion: order.AgreementVersion, PricingRuleVersion: order.PricingRuleVersion,
		BoardingCodeHash: hashSecret(boardingCode), BoardingCodeSuffix: boardingCode,
		CreatedAt: order.RequestedAt,
	}
	if order.PassengerCouponID > 0 {
		row.PassengerCouponID = &order.PassengerCouponID
	}
	return row, nil
}

func (r *rideRepo) stationID(serviceAreaID int64, placeID string) (int64, error) {
	var row struct{ ID int64 }
	query := r.data.db.Table("stations").Select("id").Where(
		"service_area_id = ? AND (station_code = ? OR station_name = ?) AND status = 1",
		serviceAreaID, placeID, placeID,
	)
	if err := query.Take(&row).Error; err == nil {
		return row.ID, nil
	}
	if parsed, err := strconv.ParseInt(placeID, 10, 64); err == nil && parsed > 0 {
		if err := r.data.db.Table("stations").Select("id").Where("service_area_id = ? AND id = ? AND status = 1", serviceAreaID, parsed).Take(&row).Error; err == nil {
			return row.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: station %s not found", biz.ErrInvalidArgument, placeID)
}

func (r *rideRepo) findOrderByIdempotency(ctx context.Context, passengerID int64, key string) (*biz.Order, error) {
	row, err := r.loadOrder(ctx, "o.passenger_id = ? AND o.idempotency_key = ?", passengerID, key)
	if err != nil {
		return nil, err
	}
	return orderFromDetail(row), nil
}

func (r *rideRepo) loadOrder(ctx context.Context, where string, args ...any) (orderDetailRow, error) {
	var row orderDetailRow
	err := r.orderDetailsQuery(ctx).Where(where, args...).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, biz.ErrNotFound
	}
	return row, err
}

func (r *rideRepo) orderDetailsQuery(ctx context.Context) *gorm.DB {
	return r.data.db.WithContext(ctx).Table("trip_orders AS o").Select(`
        o.*, o.created_at AS requested_at,
        ROUND(o.estimated_distance_km * 1000)::integer AS estimated_distance_m,
        o.estimated_duration_min * 60 AS estimated_duration_s,
        COALESCE(ROUND(o.actual_distance_km * 1000)::integer, 0) AS actual_distance_m,
        COALESCE(o.actual_duration_min * 60, 0) AS actual_duration_s,
        COALESCE(o.pickup_snapshot->>'place_id', o.pickup_snapshot->>'PlaceID') AS pickup_place_id,
        COALESCE(o.pickup_snapshot->>'name', o.pickup_snapshot->>'Name') AS pickup_name,
        COALESCE(o.pickup_snapshot->>'address', o.pickup_snapshot->>'Address') AS pickup_address,
        COALESCE((COALESCE(o.pickup_snapshot->>'longitude', o.pickup_snapshot->>'Longitude'))::double precision, 0) AS pickup_longitude,
        COALESCE((COALESCE(o.pickup_snapshot->>'latitude', o.pickup_snapshot->>'Latitude'))::double precision, 0) AS pickup_latitude,
        COALESCE(o.dropoff_snapshot->>'place_id', o.dropoff_snapshot->>'PlaceID') AS destination_place_id,
        COALESCE(o.dropoff_snapshot->>'name', o.dropoff_snapshot->>'Name') AS destination_name,
        COALESCE(o.dropoff_snapshot->>'address', o.dropoff_snapshot->>'Address') AS destination_address,
        COALESCE((COALESCE(o.dropoff_snapshot->>'longitude', o.dropoff_snapshot->>'Longitude'))::double precision, 0) AS destination_longitude,
        COALESCE((COALESCE(o.dropoff_snapshot->>'latitude', o.dropoff_snapshot->>'Latitude'))::double precision, 0) AS destination_latitude,
        vst.service_type_code, vst.display_name AS service_display_name,
		v.plate_no, v.vehicle_no, v.vehicle_model AS vehicle_model, '' AS vehicle_color,
        p.payment_status,
        COALESCE((SELECT MAX(event_id) FROM (
          SELECT (EXTRACT(EPOCH FROM created_at) * 1000000)::bigint * 100
                 + MOD(id, 50) * 2 AS event_id
          FROM trip_status_logs WHERE order_id = o.id
          UNION ALL
          SELECT (EXTRACT(EPOCH FROM reported_at) * 1000000)::bigint * 100
                 + MOD(id, 50) * 2 + 1
          FROM trip_location_logs WHERE order_id = o.id
        ) events), 0) AS last_event_id`).
		Joins("LEFT JOIN vehicle_service_types vst ON vst.id = o.service_type_id").
		Joins("LEFT JOIN vehicles v ON v.id = o.vehicle_id").
		Joins("LEFT JOIN payments p ON p.order_id = o.id")
}

func orderFromDetail(row orderDetailRow) *biz.Order {
	order := &biz.Order{
		ID: row.ID, OrderNo: row.OrderNo, PassengerID: row.PassengerID,
		ServiceAreaID: row.ServiceAreaID,
		Status:        biz.OrderStatus(row.OrderStatus),
		ServiceType: biz.ServiceType{ID: row.ServiceTypeID, Code: row.ServiceTypeCode,
			DisplayName: row.ServiceDisplayName},
		Origin: biz.Place{Name: row.PickupName, Address: row.PickupAddress,
			Longitude: row.PickupLongitude, Latitude: row.PickupLatitude},
		Destination: biz.Place{Name: row.DestinationName, Address: row.DestinationAddress,
			Longitude: row.DestinationLongitude, Latitude: row.DestinationLatitude},
		DistanceMeters: row.EstimatedDistanceM, EstimatedDurationSeconds: row.EstimatedDurationS,
		ActualDistanceMeters: row.ActualDistanceM, ActualDurationSeconds: row.ActualDurationS,
		Fare: biz.Fare{OriginalCents: int64(row.OriginalAmount),
			DiscountCents: int64(row.DiscountAmount), PayableCents: int64(row.PayableAmount)},
		RequestedAt: row.RequestedAt, ArrivedAt: row.ArrivedAt, StartedAt: row.StartedAt,
		CompletedAt: row.CompletedAt, CancelledAt: row.CancelledAt, LastEventID: row.LastEventID,
		IdempotencyKey: row.IdempotencyKey, PassengerCount: row.PassengerCount,
		AgreementVersion: row.UserAgreementVersion, QuoteID: row.QuoteID,
		PricingRuleVersion: row.PricingRuleVersion, BoardingCodeSuffix: row.BoardingCodeSuffix,
		RideMode: row.RideMode, ScheduledAt: row.ScheduledAt,
	}
	if row.VehicleID != nil {
		order.Vehicle = &biz.Vehicle{ID: *row.VehicleID, PlateNo: row.PlateNo,
			BodyNo: row.VehicleNo, Model: row.VehicleModel, Color: row.VehicleColor,
			ServiceTypeName: row.ServiceDisplayName}
	}
	if row.PaymentStatus != nil {
		order.Fare.PaymentStatus = paymentStatusText(*row.PaymentStatus)
	}
	return order
}

func commandToRow(command *biz.OrderCommand) *commandRow {
	return &commandRow{
		CommandNo: command.CommandNo, OrderID: command.OrderID,
		PassengerID: command.PassengerID, CommandType: command.Type,
		IdempotencyKey: command.IdempotencyKey, Status: 1, ResultCode: "ACCEPTED",
		ReasonCode: command.ReasonCode, ClientLongitude: command.ClientLongitude,
		ClientLatitude: command.ClientLatitude, CreatedAt: command.CreatedAt,
	}
}

func commandFromRow(row commandRow) *biz.OrderCommand {
	return &biz.OrderCommand{ID: row.ID, CommandNo: row.CommandNo, OrderID: row.OrderID,
		PassengerID: row.PassengerID, Type: row.CommandType, IdempotencyKey: row.IdempotencyKey,
		Accepted: row.Status == 1, CreatedAt: row.CreatedAt}
}

func paymentFromRow(row paymentRow) *biz.Payment {
	return &biz.Payment{ID: row.ID, PaymentNo: row.PaymentNo, OrderID: row.OrderID,
		PassengerID: row.PassengerID, IdempotencyKey: row.IdempotencyKey,
		Method: row.PaymentMethod, Amount: int64(row.Amount), Status: row.PaymentStatus,
		ChannelTradeNo: row.ProviderTradeNo, PaidAt: row.PaidAt}
}

func reviewFromRow(row reviewRow) *biz.Review {
	var tags []string
	_ = json.Unmarshal(row.Tags, &tags)
	return &biz.Review{ID: row.ID, OrderID: row.OrderID, PassengerID: row.PassengerID,
		Rating: row.OverallScore, Tags: tags, Comment: row.Comment,
		Anonymous: row.Anonymous, CreatedAt: row.CreatedAt}
}

func paymentStatusText(status int16) string {
	switch status {
	case 2:
		return "PAID"
	case 3:
		return "FAILED"
	case 6:
		return "REFUNDED"
	default:
		return "PENDING"
	}
}

var _ = fmt.Sprintf
