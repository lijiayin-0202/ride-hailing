package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

type RideService struct {
	v1.UnimplementedRideServiceServer
	uc            *biz.RideUsecase
	log           *log.Helper
	internalToken string
}

func NewRideService(uc *biz.RideUsecase, data *conf.Data, logger log.Logger) *RideService {
	return &RideService{
		uc:            uc,
		log:           log.NewHelper(logger),
		internalToken: data.GetSecurity().GetInternalToken(),
	}
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
		// 派单候选数量与预计匹配时长由模块三的派单结果回填，此处不再返回演示数字。
		Matching:     &v1.MatchingInfo{ScannedVehicleCount: 0, EstimatedMatchSeconds: 0},
		RouteSummary: routeToAPI(result), EstimatedFare: fareToAPI(result.Fare),
		EventStreamUrl: fmt.Sprintf("/api/v1/passenger/orders/%s/events", formatID("ord", result.ID)),
	}, nil
}

// GetOrderLiveView 返回活动订单聚合详情。
func (s *RideService) GetOrderLiveView(ctx context.Context, req *v1.GetOrderRequest) (*v1.OrderLiveViewReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	view, usecaseErr := s.uc.GetLiveView(ctx, passenger, orderID)
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return liveViewToAPI(view), nil
}

// GetOrderSettlement 返回结算聚合视图。
func (s *RideService) GetOrderSettlement(ctx context.Context, req *v1.GetOrderRequest) (*v1.SettlementReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	settlement, usecaseErr := s.uc.GetSettlement(ctx, passenger, orderID)
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return settlementToAPI(settlement), nil
}

// CancelOrder 取消订单，返回取消费用与可退金额。
func (s *RideService) CancelOrder(ctx context.Context, req *v1.CancelOrderRequest) (*v1.CancelOrderReply, error) {
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	cancellation, usecaseErr := s.uc.CancelOrder(ctx, passenger, orderID, req.GetReasonCode())
	if usecaseErr != nil {
		return nil, serviceError(usecaseErr)
	}
	return cancellationToAPI(cancellation), nil
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
	commandType := strings.ToUpper(req.GetCommandType())
	if commandType == biz.CommandCancel {
		return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "use /api/v1/passenger/orders/{id}/cancel")
	}
	if !biz.IsPassengerCommand(commandType) && !s.internalCaller(ctx) {
		return nil, kratoserrors.Forbidden("FORBIDDEN", "internal token required for this command")
	}
	command := &biz.OrderCommand{
		OrderID: orderID, PassengerID: passenger, Type: commandType,
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
	return &v1.CreatePaymentReply{
		PaymentId: formatID("pay", payment.ID), PaymentNo: payment.PaymentNo,
		Amount: toYuan(payment.Amount), Currency: "CNY", PaymentStatus: paymentStatusText(payment.Status),
	}, nil
}

// HandlePaymentCallback 处理支付渠道回调，重复回调按幂等返回。
func (s *RideService) HandlePaymentCallback(ctx context.Context, req *v1.PaymentCallbackRequest) (*v1.PaymentCallbackReply, error) {
	if !s.internalCaller(ctx) {
		return nil, kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	occurredAt := time.Now()
	if req.GetOccurredAt() != "" {
		parsed, err := time.Parse(time.RFC3339, req.GetOccurredAt())
		if err != nil {
			return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "occurredAt must be RFC3339")
		}
		occurredAt = parsed
	}
	result, err := s.uc.HandlePaymentCallback(ctx, &biz.PaymentCallback{
		PaymentNo:      req.GetPaymentNo(),
		ChannelTradeNo: req.GetChannelTradeNo(),
		Result:         req.GetResult(),
		AmountCents:    int64(math.Round(req.GetAmount() * 100)),
		OccurredAt:     occurredAt,
	})
	if err != nil {
		return nil, serviceError(err)
	}
	return &v1.PaymentCallbackReply{
		PaymentId: formatID("pay", result.Payment.ID), PaymentNo: result.Payment.PaymentNo,
		PaymentStatus: paymentStatusText(result.Payment.Status),
		OrderId:       formatID("ord", result.OrderID), Amount: toYuan(result.Payment.Amount),
		Duplicated: result.Duplicated,
	}, nil
}

func (s *RideService) SubmitReview(ctx context.Context, req *v1.SubmitReviewRequest) (*v1.SubmitReviewReply, error) {
	if requestHeader(ctx, "X-Idempotency-Key") == "" {
		return nil, kratoserrors.BadRequest("INVALID_ARGUMENT", "missing X-Idempotency-Key")
	}
	passenger, orderID, err := authenticatedOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, err
	}
	review, usecaseErr := s.uc.SubmitReview(ctx, &biz.Review{
		OrderID: orderID, PassengerID: passenger, Rating: req.GetRating(),
		Tags: req.GetTagCodes(), Comment: req.GetComment(), Anonymous: req.GetAnonymous(),
	})
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
			DepartureAt: order.RequestedAt.Format(time.RFC3339), VehicleDisplayName: vehicleDisplayName(order),
			PaidAmount: toYuan(order.Fare.PayableCents), Currency: "CNY", Status: status, StatusText: text,
		})
	}
	return reply, nil
}

// vehicleDisplayName 优先使用订单关联车辆信息，缺失时回退车型名称。
func vehicleDisplayName(order *biz.Order) string {
	if order.Vehicle != nil {
		if order.Vehicle.PlateNo != "" {
			return order.Vehicle.PlateNo
		}
		if order.Vehicle.Model != "" {
			return order.Vehicle.Model
		}
	}
	return order.ServiceType.DisplayName
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

// internalCaller 校验内部令牌。未配置令牌时（本地开发）放行，配置后必须匹配。
func (s *RideService) internalCaller(ctx context.Context) bool {
	if s.internalToken == "" {
		return true
	}
	return requestHeader(ctx, "X-Internal-Token") == s.internalToken
}
