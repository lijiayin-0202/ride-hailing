package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type RideService struct {
	v1.UnimplementedRideServiceServer
	uc  *biz.RideUsecase
	log *log.Helper
}

func NewRideService(uc *biz.RideUsecase, logger log.Logger) *RideService {
	return &RideService{uc: uc, log: log.NewHelper(logger)}
}

func (s *RideService) EstimateFare(ctx context.Context, req *v1.EstimateFareRequest) (*v1.EstimateFareReply, error) {
	quote := &biz.Quote{
		PassengerID: passengerID(ctx, false), CityCode: req.GetCityCode(),
		RideMode: strings.ToUpper(req.GetRideMode()), Origin: placeFromAPI(req.GetOrigin()),
		Destination: placeFromAPI(req.GetDestination()),
	}
	if req.GetScheduledAt() != "" {
		value, err := time.Parse(time.RFC3339, req.GetScheduledAt())
		if err != nil {
			return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "scheduledAt must be RFC3339")
		}
		quote.ScheduledAt = &value
	}
	result, err := s.uc.EstimateFare(ctx, quote, req.GetCouponId())
	if err != nil {
		return nil, serviceError(err)
	}
	return quoteToAPI(result), nil
}

func (s *RideService) CreateOrder(ctx context.Context, req *v1.CreateOrderRequest) (*v1.CreateOrderReply, error) {
	passenger := passengerID(ctx, true)
	if passenger == 0 {
		return nil, kratoserrors.Unauthorized("UNAUTHORIZED", "missing passenger identity")
	}
	order := &biz.Order{PassengerCount: req.GetPassengerCount(), ContactPhone: req.GetContactPhone(), AgreementVersion: req.GetUserAgreementVersion()}
	idempotencyKey := requestHeader(ctx, "X-Idempotency-Key")
	if idempotencyKey == "" {
		return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "missing X-Idempotency-Key")
	}
	result, err := s.uc.CreateOrder(ctx, passenger, idempotencyKey, order, req.GetQuoteToken(), req.GetServiceTypeId())
	if err != nil {
		return nil, serviceError(err)
	}
	status, _, _, _ := biz.StatusPresentation(result.Status)
	return &v1.CreateOrderReply{
		OrderId: formatID("ord", result.ID), OrderNo: result.OrderNo, Status: status,
		Matching:     &v1.MatchingInfo{ScannedVehicleCount: 0, EstimatedMatchSeconds: 3},
		RouteSummary: routeToAPI(result), EstimatedFare: fareToAPI(result.Fare),
		EventStreamUrl: fmt.Sprintf("/api/v1/orders/%s/events", formatID("ord", result.ID)),
	}, nil
}

func (s *RideService) GetOrder(ctx context.Context, req *v1.GetOrderRequest) (*v1.OrderDetailReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	order, usecaseErr := s.uc.GetOrder(ctx, passenger, orderID)
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return orderToAPI(order), nil
}

func (s *RideService) StreamOrderEvents(req *v1.StreamOrderEventsRequest, stream v1.RideService_StreamOrderEventsServer) error {
	passenger, orderID, err := authenticatedOrder(stream.Context(), req.GetOrderId())
	if err != nil {
		return err
	}
	afterID, _ := strconv.ParseInt(req.GetLastEventId(), 10, 64)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		events, listErr := s.uc.ListEvents(stream.Context(), passenger, orderID, afterID)
		if listErr != nil {
			return serviceError(listErr)
		}
		for _, event := range events {
			if err := stream.Send(eventToAPI(event)); err != nil {
				return err
			}
			afterID = event.ID
		}
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
		}
	}
}

func (s *RideService) ExecuteOrderCommand(ctx context.Context, req *v1.ExecuteOrderCommandRequest) (*v1.ExecuteOrderCommandReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	command := &biz.OrderCommand{
		OrderID: orderID, PassengerID: passenger, Type: strings.ToUpper(req.GetCommandType()),
		IdempotencyKey: requestHeader(ctx, "X-Idempotency-Key"), BoardingCode: req.GetBoardingCode(),
		ReasonCode: req.GetReasonCode(), ClientLongitude: req.GetClientLongitude(), ClientLatitude: req.GetClientLatitude(),
	}
	result, usecaseErr := s.uc.ExecuteCommand(ctx, command)
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	status, text, _, _ := biz.StatusPresentation(result.ResultStatus)
	return &v1.ExecuteOrderCommandReply{CommandId: formatID("ocmd", result.ID), Accepted: result.Accepted, OrderStatus: status, StatusText: text}, nil
}

func (s *RideService) CreatePayment(ctx context.Context, req *v1.CreatePaymentRequest) (*v1.CreatePaymentReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	idempotencyKey := requestHeader(ctx, "X-Idempotency-Key")
	if idempotencyKey == "" {
		return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "missing X-Idempotency-Key")
	}
	payment, usecaseErr := s.uc.CreatePayment(ctx, passenger, orderID, req.GetPaymentMethod(), idempotencyKey)
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return &v1.CreatePaymentReply{PaymentId: formatID("pay", payment.ID), PaymentNo: payment.PaymentNo, Amount: toYuan(payment.Amount), Currency: "CNY", PaymentStatus: paymentStatus(payment.Status)}, nil
}

func (s *RideService) SubmitReview(ctx context.Context, req *v1.SubmitReviewRequest) (*v1.SubmitReviewReply, error) {
	if requestHeader(ctx, "X-Idempotency-Key") == "" {
		return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "missing X-Idempotency-Key")
	}
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	review, usecaseErr := s.uc.SubmitReview(ctx, &biz.Review{OrderID: orderID, PassengerID: passenger, Rating: req.GetRating(), Tags: req.GetTagCodes(), Comment: req.GetComment()})
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return &v1.SubmitReviewReply{ReviewId: formatID("rvw", review.ID), SubmittedAt: review.CreatedAt.Format(time.RFC3339)}, nil
}

func (s *RideService) ListOrderHistory(ctx context.Context, req *v1.ListOrderHistoryRequest) (*v1.ListOrderHistoryReply, error) {
	passenger := passengerID(ctx, true)
	if passenger == 0 {
		return nil, kratoserrors.Unauthorized("UNAUTHORIZED", "missing passenger identity")
	}
	page, err := s.uc.ListHistory(ctx, passenger, req.GetCursor(), int(req.GetPageSize()), req.GetStatusGroup())
	if err != nil {
		return nil, serviceError(err)
	}
	reply := &v1.ListOrderHistoryReply{HasMore: page.HasMore}
	if page.Next != nil {
		reply.NextCursor = biz.EncodeHistoryCursor(*page.Next)
	}
	for _, order := range page.Items {
		status, text, _, _ := biz.StatusPresentation(order.Status)
		reply.Items = append(reply.Items, &v1.OrderHistoryItem{
			OrderId: formatID("ord", order.ID), OrderNo: order.OrderNo,
			OriginName: order.Origin.Name, DestinationName: order.Destination.Name,
			DepartureAt: order.RequestedAt.Format(time.RFC3339), VehicleDisplayName: order.ServiceType.DisplayName,
			PaidAmount: toYuan(order.Fare.PayableCents), Currency: "CNY", Status: status, StatusText: text,
		})
	}
	return reply, nil
}

func (s *RideService) StreamOrderEventsHTTP(ctx kratoshttp.Context) error {
	passenger, orderID, err := authenticatedOrder(ctx, ctx.Vars().Get("order_id"))
	if err != nil {
		return err
	}
	afterValue := ctx.Header().Get("Last-Event-ID")
	if afterValue == "" {
		afterValue = ctx.Query().Get("lastEventId")
	}
	afterID, _ := strconv.ParseInt(afterValue, 10, 64)
	response := ctx.Response()
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	flusher, ok := response.(http.Flusher)
	if !ok {
		return kratoserrors.InternalServer("INTERNAL_ERROR", "streaming is unsupported")
	}
	return s.serveSSE(ctx, response, flusher, passenger, orderID, afterID)
}

func (s *RideService) serveSSE(ctx kratoshttp.Context, response http.ResponseWriter, flusher http.Flusher, passenger, orderID, afterID int64) error {
	ticker := time.NewTicker(time.Second)
	keepalive := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer keepalive.Stop()
	for {
		events, listErr := s.uc.ListEvents(ctx, passenger, orderID, afterID)
		if listErr != nil {
			return serviceError(listErr)
		}
		for _, event := range events {
			payload, _ := json.Marshal(eventToAPI(event))
			if _, writeErr := fmt.Fprintf(response, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.EventType, payload); writeErr != nil {
				return writeErr
			}
			afterID = event.ID
			flusher.Flush()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-keepalive.C:
			_, _ = fmt.Fprint(response, ": keepalive\n\n")
			flusher.Flush()
		case <-ticker.C:
		}
	}
}
