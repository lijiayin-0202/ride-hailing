package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func authenticatedOrder(ctx context.Context, value string) (int64, int64, error) {
	passenger := passengerID(ctx, true)
	if passenger == 0 {
		return 0, 0, kratoserrors.Unauthorized("UNAUTHORIZED", "missing passenger identity")
	}
	orderID, err := parseID(value)
	if err != nil {
		return 0, 0, kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid orderId")
	}
	return passenger, orderID, nil
}

func passengerID(ctx context.Context, required bool) int64 {
	value := requestHeader(ctx, "X-Passenger-ID")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil && required {
		return 0
	}
	return id
}

func requestHeader(ctx context.Context, key string) string {
	if tr, ok := transport.FromServerContext(ctx); ok {
		return tr.RequestHeader().Get(key)
	}
	return ""
}

func parseID(value string) (int64, error) {
	parts := strings.Split(value, "_")
	return strconv.ParseInt(parts[len(parts)-1], 10, 64)
}

func serviceError(err error) error {
	switch {
	case errors.Is(err, biz.ErrInvalidArgument):
		return kratoserrors.BadRequest("INVALID_ARGUMENT", err.Error())
	case errors.Is(err, biz.ErrNotFound):
		return kratoserrors.NotFound("RESOURCE_NOT_FOUND", "resource not found")
	case errors.Is(err, biz.ErrConflict):
		return kratoserrors.Conflict("BUSINESS_CONFLICT", err.Error())
	default:
		return kratoserrors.InternalServer("INTERNAL_ERROR", "internal service error")
	}
}

func placeFromAPI(place *v1.Place) biz.Place {
	if place == nil {
		return biz.Place{}
	}
	return biz.Place{PlaceID: place.GetPlaceId(), Name: place.GetName(), Address: place.GetAddress(), Longitude: place.GetLongitude(), Latitude: place.GetLatitude()}
}

func quoteToAPI(quote *biz.Quote) *v1.EstimateFareReply {
	reply := &v1.EstimateFareReply{QuoteToken: quote.Token, ExpiresAt: quote.ExpiresAt.Format(time.RFC3339), Route: &v1.RouteSummary{OriginName: quote.Origin.Name, DestinationName: quote.Destination.Name, DistanceMeters: quote.DistanceMeters, EstimatedDurationSeconds: quote.EstimatedDurationSeconds, Polyline: quote.Polyline}}
	for _, option := range quote.Options {
		reply.Quotes = append(reply.Quotes, &v1.ServiceQuote{
			ServiceTypeId: option.ServiceType.Code, DisplayName: option.ServiceType.DisplayName,
			Tags: option.ServiceType.Tags, SeatCount: option.ServiceType.SeatCount,
			PickupEtaSeconds: option.ServiceType.PickupETASeconds, Fare: fareToAPI(option.Fare),
			AvailableVehicleCount: option.AvailableVehicles,
		})
	}
	if quote.Coupon != nil {
		reply.AppliedCoupon = &v1.AppliedCoupon{CouponId: formatID("ucp", quote.Coupon.PassengerCoupon), CouponName: quote.Coupon.Name, DiscountAmount: toYuan(quote.Coupon.DiscountCents), ValidUntil: quote.Coupon.ValidUntil.Format(time.RFC3339)}
	}
	return reply
}

// liveViewToAPI 组装活动订单聚合详情。
func liveViewToAPI(view *biz.OrderLiveView) *v1.OrderLiveViewReply {
	order := view.Order
	status, text, step, actions := biz.StatusPresentation(order.Status)
	reply := &v1.OrderLiveViewReply{
		OrderId: formatID("ord", order.ID), OrderNo: order.OrderNo,
		Status: status, StatusText: text, Step: step,
		Route: routeToAPI(order), Fare: fareToAPI(order.Fare),
		AvailableActions: actions, LastEventId: strconv.FormatInt(order.LastEventID, 10),
	}
	if order.Vehicle != nil {
		reply.Vehicle = &v1.VehicleInfo{
			VehicleId: formatID("veh", order.Vehicle.ID), PlateNo: order.Vehicle.PlateNo,
			Model: order.Vehicle.Model, ServiceTypeName: order.Vehicle.ServiceTypeName,
			BodyNo: order.Vehicle.BodyNo, Color: order.Vehicle.Color,
		}
	}
	reply.Pickup = &v1.PickupInfo{BoardingCodeSuffix: order.BoardingCodeSuffix}
	if order.ArrivedAt != nil {
		reply.Pickup.ArrivedAt = order.ArrivedAt.Format(time.RFC3339)
		reply.Pickup.WaitDeadline = order.ArrivedAt.Add(3 * time.Minute).Format(time.RFC3339)
	}
	progress := biz.BuildTripProgress(order, view.Location)
	reply.TripProgress = &v1.TripProgress{
		RemainingDistanceMeters:  progress.RemainingDistanceMeters,
		RemainingDurationSeconds: progress.RemainingDurationSeconds,
		SpeedKmh:                 progress.SpeedKMH,
		ProgressPercent:          progress.ProgressPercent,
	}
	if view.Location != nil {
		reply.Location = &v1.LatestLocation{
			Longitude: view.Location.Longitude, Latitude: view.Location.Latitude,
			SpeedKmh: view.Location.SpeedKMH, ReportedAt: formatTime(&view.Location.ReportedAt),
		}
	}
	if view.Dispatch != nil {
		reply.Dispatch = &v1.DispatchSummary{
			Strategy: view.Dispatch.Strategy, StrategyVersion: view.Dispatch.StrategyVersion,
			DispatchStatus: view.Dispatch.Status, VehicleResponse: view.Dispatch.VehicleResponse,
			AttemptCount: view.Dispatch.AttemptCount, PickupEtaSeconds: view.Dispatch.PickupETASeconds,
			PickupDistanceMeters: view.Dispatch.PickupDistanceMeters,
			DispatchedAt:         formatTime(view.Dispatch.DispatchedAt),
		}
	}
	if view.Boarding != nil {
		reply.Boarding = &v1.BoardingVerification{
			Method: view.Boarding.Method, Status: view.Boarding.Status,
			VerifiedAt: formatTime(view.Boarding.VerifiedAt), Attempts: view.Boarding.Attempts,
		}
	}
	for _, event := range view.Safety {
		reply.SafetyEvents = append(reply.SafetyEvents, &v1.SafetyEvent{
			EventId: strconv.FormatInt(event.ID, 10), Type: event.Type, Level: event.Level,
			Description: event.Description, Status: event.Status,
			OccurredAt: formatTime(&event.OccurredAt),
		})
	}
	return reply
}

// settlementToAPI 组装结算视图。
func settlementToAPI(settlement *biz.Settlement) *v1.SettlementReply {
	order := settlement.Order
	status, text, _, _ := biz.StatusPresentation(order.Status)
	reply := &v1.SettlementReply{
		OrderId: formatID("ord", order.ID), OrderNo: order.OrderNo,
		Status: status, StatusText: text,
		PayableAmount: toYuan(order.Fare.PayableCents), PaidAmount: toYuan(settlement.PaidCents),
		RefundedAmount: toYuan(settlement.RefundedCents), Currency: "CNY",
	}
	for _, item := range settlement.FeeItems {
		reply.FeeItems = append(reply.FeeItems, &v1.FareItem{Type: item.Type, Name: item.Name, Amount: toYuan(item.AmountCents)})
	}
	if settlement.Payment != nil {
		reply.Payment = &v1.SettlementPayment{
			PaymentId: formatID("pay", settlement.Payment.PaymentID),
			PaymentNo: settlement.Payment.PaymentNo, Method: settlement.Payment.Method,
			Status: settlement.Payment.Status, Amount: toYuan(settlement.Payment.AmountCents),
			PaidAt: formatTime(settlement.Payment.PaidAt),
		}
	}
	if settlement.Review != nil {
		reply.Review = &v1.SettlementReview{
			Submitted: settlement.Review.Submitted, Rating: settlement.Review.Rating,
			Tags: settlement.Review.Tags, SubmittedAt: formatTime(settlement.Review.SubmittedAt),
		}
	}
	return reply
}

func cancellationToAPI(cancellation *biz.Cancellation) *v1.CancelOrderReply {
	status, text, _, _ := biz.StatusPresentation(biz.OrderStatusCancelled)
	return &v1.CancelOrderReply{
		OrderId: formatID("ord", cancellation.OrderID), OrderStatus: status, StatusText: text,
		CancelFeeAmount: toYuan(cancellation.FeeCents), CancelFeeRule: cancellation.RuleCode,
		CancelFeeDescription: biz.CancelRuleText(cancellation.RuleCode),
		RefundAmount:         toYuan(cancellation.RefundCents), Currency: "CNY",
		CancelledAt: formatTime(&cancellation.CancelledAt),
	}
}

func routeToAPI(order *biz.Order) *v1.RouteSummary {
	return &v1.RouteSummary{OriginName: order.Origin.Name, DestinationName: order.Destination.Name, DistanceMeters: order.DistanceMeters, EstimatedDurationSeconds: order.EstimatedDurationSeconds}
}

func fareToAPI(fare biz.Fare) *v1.Fare {
	result := &v1.Fare{OriginalAmount: toYuan(fare.OriginalCents), DiscountAmount: toYuan(fare.DiscountCents), PayableAmount: toYuan(fare.PayableCents), Currency: "CNY", PaymentStatus: fare.PaymentStatus}
	for _, item := range fare.Items {
		result.Breakdown = append(result.Breakdown, &v1.FareItem{Type: item.Type, Name: item.Name, Amount: toYuan(item.AmountCents)})
	}
	return result
}

func eventToAPI(event biz.OrderEvent) *v1.OrderEvent {
	status, text, _, _ := biz.StatusPresentation(event.Status)
	return &v1.OrderEvent{EventId: strconv.FormatInt(event.ID, 10), EventType: event.EventType, OccurredAt: event.OccurredAt.Format(time.RFC3339Nano), OrderStatus: status, StatusText: text, Longitude: event.Longitude, Latitude: event.Latitude, SpeedKmh: event.SpeedKMH}
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatID(prefix string, id int64) string { return fmt.Sprintf("%s_%d", prefix, id) }
func toYuan(value int64) float64              { return float64(value) / 100 }

func paymentStatusText(status int16) string {
	return map[int16]string{
		biz.PaymentStatusPending:           "PENDING",
		biz.PaymentStatusProcessing:        "PROCESSING",
		biz.PaymentStatusPaid:              "PAID",
		biz.PaymentStatusFailed:            "FAILED",
		biz.PaymentStatusClosed:            "CLOSED",
		biz.PaymentStatusPartiallyRefunded: "PARTIAL_REFUNDED",
		biz.PaymentStatusRefunded:          "REFUNDED",
	}[status]
}
