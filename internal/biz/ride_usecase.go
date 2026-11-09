package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (uc *RideUsecase) EstimateFare(ctx context.Context, quote *Quote, couponID string) (*Quote, error) {
	now := uc.now()
	if err := validateQuote(quote, now); err != nil {
		return nil, err
	}
	serviceAreaID, err := uc.repo.ResolveServiceArea(ctx, quote.CityCode, quote.Origin, quote.Destination)
	if err != nil {
		return nil, err
	}
	serviceTypes, err := uc.repo.ListServiceTypes(ctx, quote.CityCode, now)
	if err != nil {
		return nil, err
	}
	if len(serviceTypes) == 0 {
		return nil, fmt.Errorf("%w: no available service type", ErrConflict)
	}
	quote.ServiceAreaID = serviceAreaID
	quote.DistanceMeters, quote.EstimatedDurationSeconds = estimateRoute(quote.Origin, quote.Destination)
	quote.Token = newPublicID("qt", now)
	quote.ExpiresAt = now.Add(2 * time.Minute)

	originalMax := int64(0)
	for _, serviceType := range serviceTypes {
		fare := calculateFare(serviceType.Rule, quote.DistanceMeters, nil)
		if fare.OriginalCents > originalMax {
			originalMax = fare.OriginalCents
		}
	}
	var coupon *QuoteCoupon
	if couponID != "" && quote.PassengerID > 0 {
		coupon, err = uc.repo.FindCoupon(ctx, quote.PassengerID, couponID, originalMax)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrNotFound) {
			// 优惠券不可用时降级为无券方案，避免试算整体失败。
			coupon = nil
		}
	}
	quote.Coupon = coupon

	for _, serviceType := range serviceTypes {
		fare := calculateFare(serviceType.Rule, quote.DistanceMeters, coupon)
		available, countErr := uc.repo.CountAvailableVehicles(ctx, serviceAreaID, serviceType.ID)
		if countErr != nil {
			return nil, countErr
		}
		serviceType.PickupETASeconds = pickupETA(serviceType.PickupETASeconds, available)
		quote.Options = append(quote.Options, QuoteOption{
			ServiceType: serviceType, Fare: fare, AvailableVehicles: available,
		})
	}
	if err := uc.repo.CreateQuote(ctx, quote); err != nil {
		return nil, err
	}
	return quote, nil
}

// pickupETA 在可用车为 0 时拉长预期接驾时间，避免返回"3 秒接驾"这类演示数字。
func pickupETA(base int32, available int32) int32 {
	if available > 0 {
		return base
	}
	if base < 600 {
		return 600
	}
	return base
}

func (uc *RideUsecase) CreateOrder(ctx context.Context, passengerID int64, idempotencyKey string, input *Order, quoteToken, serviceTypeCode string) (*Order, error) {
	if passengerID <= 0 || idempotencyKey == "" || len(idempotencyKey) > 128 || input.PassengerCount <= 0 || !validPhone(input.ContactPhone) || input.AgreementVersion == "" {
		return nil, fmt.Errorf("%w: missing order field", ErrInvalidArgument)
	}
	// 幂等优先：重复提交时直接返回首单，不再校验已过期的试算凭证。
	if existing, findErr := uc.repo.FindOrderByIdempotency(ctx, passengerID, idempotencyKey); findErr == nil {
		return existing, nil
	} else if !errors.Is(findErr, ErrNotFound) {
		return nil, findErr
	}
	quote, err := uc.repo.GetQuote(ctx, quoteToken)
	if err != nil {
		return nil, err
	}
	if quote.ExpiresAt.Before(uc.now()) || quote.PassengerID != 0 && quote.PassengerID != passengerID {
		return nil, fmt.Errorf("%w: quote expired or does not belong to passenger", ErrConflict)
	}
	option, ok := findQuoteOption(quote.Options, serviceTypeCode)
	if !ok || input.PassengerCount > option.ServiceType.SeatCount {
		return nil, fmt.Errorf("%w: invalid service type or passenger count", ErrInvalidArgument)
	}
	input.PassengerID = passengerID
	input.ServiceAreaID = quote.ServiceAreaID
	input.IdempotencyKey = idempotencyKey
	input.Status = OrderStatusPendingDispatch
	input.ServiceType = option.ServiceType
	input.Origin, input.Destination = quote.Origin, quote.Destination
	input.DistanceMeters = quote.DistanceMeters
	input.EstimatedDurationSeconds = quote.EstimatedDurationSeconds
	input.Fare = option.Fare
	if len(input.Fare.Items) == 0 {
		input.Fare.Items = BuildFareItems(option.ServiceType.Rule, input.Fare.OriginalCents, input.Fare.DiscountCents)
	}
	input.QuoteID = quote.ID
	if quote.Coupon != nil {
		input.PassengerCouponID = quote.Coupon.PassengerCoupon
	}
	input.PricingRuleVersion = option.ServiceType.Rule.Version
	input.RideMode, input.ScheduledAt = quote.RideMode, quote.ScheduledAt
	input.RequestedAt = uc.now()
	created, _, err := uc.repo.CreateOrder(ctx, input)
	return created, err
}

func (uc *RideUsecase) GetOrder(ctx context.Context, passengerID, orderID int64) (*Order, error) {
	if passengerID <= 0 || orderID <= 0 {
		return nil, ErrInvalidArgument
	}
	return uc.repo.GetOrder(ctx, passengerID, orderID)
}

// GetLiveView 返回活动订单聚合详情：订单、车辆、最新位置、派单、上车核验与安全事件摘要。
func (uc *RideUsecase) GetLiveView(ctx context.Context, passengerID, orderID int64) (*OrderLiveView, error) {
	if passengerID <= 0 || orderID <= 0 {
		return nil, ErrInvalidArgument
	}
	return uc.repo.GetLiveView(ctx, passengerID, orderID)
}

// GetSettlement 返回结算聚合：订单、费用明细、支付单与评价状态。
func (uc *RideUsecase) GetSettlement(ctx context.Context, passengerID, orderID int64) (*Settlement, error) {
	if passengerID <= 0 || orderID <= 0 {
		return nil, ErrInvalidArgument
	}
	settlement, err := uc.repo.GetSettlement(ctx, passengerID, orderID)
	if err != nil {
		return nil, err
	}
	if !settlement.Order.Status.Terminal() {
		return nil, fmt.Errorf("%w: order is not settled yet", ErrConflict)
	}
	return settlement, nil
}

func (uc *RideUsecase) ExecuteCommand(ctx context.Context, command *OrderCommand) (*OrderCommand, error) {
	if command.PassengerID <= 0 || command.OrderID <= 0 || command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return nil, ErrInvalidArgument
	}
	if !IsKnownCommand(command.Type) {
		return nil, fmt.Errorf("%w: unsupported command %s", ErrInvalidArgument, command.Type)
	}
	order, err := uc.repo.GetOrder(ctx, command.PassengerID, command.OrderID)
	if err != nil {
		return nil, err
	}
	next, err := commandTarget(order.Status, command.Type)
	if err != nil {
		return nil, err
	}
	if command.Type == CommandUnlockAndStart && command.BoardingCode == "" {
		return nil, fmt.Errorf("%w: boarding code is required", ErrInvalidArgument)
	}
	command.ResultStatus = next
	command.CurrentStatus = order.Status
	command.CommandNo = newPublicID("ocmd", uc.now())
	command.CreatedAt = uc.now()
	return uc.repo.ExecuteCommand(ctx, command)
}

// CancelOrder 计算取消费用与可退金额并落库，重复取消返回首次结果（幂等）。
func (uc *RideUsecase) CancelOrder(ctx context.Context, passengerID, orderID int64, reasonCode string) (*Cancellation, error) {
	if passengerID <= 0 || orderID <= 0 {
		return nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrder(ctx, passengerID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status == OrderStatusCancelled {
		return uc.repo.CancelOrder(ctx, &Cancellation{OrderID: orderID, PassengerID: passengerID})
	}
	fee, rule, err := CancelFee(order.Status, order.Fare.PayableCents)
	if err != nil {
		return nil, err
	}
	paid, err := uc.repo.PaidAmount(ctx, orderID)
	if err != nil {
		return nil, err
	}
	reason := strings.ToUpper(strings.TrimSpace(reasonCode))
	if reason == "" {
		reason = "PASSENGER_CANCEL"
	}
	cancellation := &Cancellation{
		OrderID: orderID, PassengerID: passengerID, ReasonCode: reason,
		RuleCode: rule, FeeCents: fee, RefundCents: RefundableCents(paid, fee),
		PaidCents: paid, CancelledAt: uc.now(), Status: OrderStatusCancelled,
	}
	return uc.repo.CancelOrder(ctx, cancellation)
}

func (uc *RideUsecase) ListEvents(ctx context.Context, passengerID, orderID, afterID int64) ([]OrderEvent, error) {
	if _, err := uc.repo.GetOrder(ctx, passengerID, orderID); err != nil {
		return nil, err
	}
	return uc.repo.ListEvents(ctx, passengerID, orderID, afterID)
}

func (uc *RideUsecase) CreatePayment(ctx context.Context, passengerID, orderID int64, method, idempotencyKey string) (*Payment, error) {
	if len(idempotencyKey) == 0 || len(idempotencyKey) > 128 {
		return nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrder(ctx, passengerID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != OrderStatusCompleted {
		return nil, fmt.Errorf("%w: order is not completed", ErrConflict)
	}
	if order.Fare.PayableCents <= 0 {
		return nil, fmt.Errorf("%w: order payable amount is invalid", ErrConflict)
	}
	methodCode, ok := paymentMethodCode(method)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported payment method", ErrInvalidArgument)
	}
	payment := &Payment{OrderID: order.ID, PassengerID: passengerID, IdempotencyKey: idempotencyKey, PaymentNo: newPublicID("pay", uc.now()), Method: methodCode, Amount: order.Fare.PayableCents, Status: PaymentStatusPending}
	created, _, err := uc.repo.CreatePayment(ctx, payment)
	return created, err
}

func paymentMethodCode(method string) (int16, bool) {
	code, ok := map[string]int16{"ALIPAY": 1, "WECHAT": 2, "BALANCE": 3, "MOCK": 4}[strings.ToUpper(method)]
	return code, ok
}

// HandlePaymentCallback 收敛渠道支付回调，重复回调按幂等处理。
func (uc *RideUsecase) HandlePaymentCallback(ctx context.Context, callback *PaymentCallback) (*PaymentCallbackResult, error) {
	if callback == nil || callback.PaymentNo == "" {
		return nil, fmt.Errorf("%w: payment no is required", ErrInvalidArgument)
	}
	result := strings.ToUpper(strings.TrimSpace(callback.Result))
	if result != "SUCCESS" && result != "FAILED" {
		return nil, fmt.Errorf("%w: unsupported callback result", ErrInvalidArgument)
	}
	if result == "SUCCESS" && callback.AmountCents <= 0 {
		return nil, fmt.Errorf("%w: callback amount is required", ErrInvalidArgument)
	}
	callback.Result = result
	if callback.OccurredAt.IsZero() {
		callback.OccurredAt = uc.now()
	}
	return uc.repo.HandlePaymentCallback(ctx, callback)
}

func (uc *RideUsecase) SubmitReview(ctx context.Context, review *Review) (*Review, error) {
	if review.Rating < 1 || review.Rating > 5 || len([]rune(review.Comment)) > 500 || !validReviewTags(review.Tags) {
		return nil, fmt.Errorf("%w: invalid review", ErrInvalidArgument)
	}
	order, err := uc.repo.GetOrder(ctx, review.PassengerID, review.OrderID)
	if err != nil {
		return nil, err
	}
	if order.Status != OrderStatusCompleted {
		return nil, fmt.Errorf("%w: order is not completed", ErrConflict)
	}
	review.CreatedAt = uc.now()
	created, duplicated, err := uc.repo.CreateReview(ctx, review)
	if err != nil {
		return nil, err
	}
	if duplicated {
		return nil, fmt.Errorf("%w: review already submitted", ErrConflict)
	}
	return created, nil
}

func validReviewTags(tags []string) bool {
	allowed := map[string]struct{}{
		"SMOOTH": {}, "CLEAN": {}, "ON_TIME": {}, "SAFE": {}, "COMFORTABLE": {},
	}
	for _, tag := range tags {
		if _, ok := allowed[tag]; !ok {
			return false
		}
	}
	return true
}

func (uc *RideUsecase) ListHistory(ctx context.Context, passengerID int64, cursor string, pageSize int, statusGroup string) (*HistoryPage, error) {
	if pageSize == 0 {
		pageSize = 20
	}
	if pageSize < 1 || pageSize > 50 {
		return nil, fmt.Errorf("%w: page size must be between 1 and 50", ErrInvalidArgument)
	}
	group := strings.ToUpper(statusGroup)
	if group == "" {
		group = "ALL"
	}
	if group != "ALL" && group != "COMPLETED" && group != "CANCELLED" {
		return nil, fmt.Errorf("%w: invalid status group", ErrInvalidArgument)
	}
	decoded, err := DecodeHistoryCursor(cursor)
	if err != nil {
		return nil, err
	}
	return uc.repo.ListHistory(ctx, passengerID, decoded, pageSize, group)
}
