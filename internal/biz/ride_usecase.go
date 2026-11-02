package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (uc *RideUsecase) EstimateFare(ctx context.Context, quote *Quote, couponID string) (*Quote, error) {
	if err := validateQuote(quote, uc.now()); err != nil {
		return nil, err
	}
	serviceTypes, err := uc.repo.ListServiceTypes(ctx, quote.CityCode, uc.now())
	if err != nil {
		return nil, err
	}
	if len(serviceTypes) == 0 {
		return nil, fmt.Errorf("%w: no available service type", ErrConflict)
	}
	quote.DistanceMeters, quote.EstimatedDurationSeconds = estimateRoute(quote.Origin, quote.Destination)
	quote.Token = newPublicID("qt", uc.now())
	quote.ExpiresAt = uc.now().Add(2 * time.Minute)
	for _, serviceType := range serviceTypes {
		fare := calculateFare(serviceType.Rule, quote.DistanceMeters, nil)
		var coupon *Coupon
		if couponID != "" && quote.PassengerID > 0 {
			coupon, err = uc.repo.FindCoupon(ctx, quote.PassengerID, couponID, fare.OriginalCents)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return nil, err
			}
			if coupon != nil {
				quote.Coupon = coupon
			}
			fare = calculateFare(serviceType.Rule, quote.DistanceMeters, coupon)
		}
		quote.Options = append(quote.Options, QuoteOption{ServiceType: serviceType, Fare: fare})
	}
	if err := uc.repo.CreateQuote(ctx, quote); err != nil {
		return nil, err
	}
	return quote, nil
}

func (uc *RideUsecase) CreateOrder(ctx context.Context, passengerID int64, idempotencyKey string, input *Order, quoteToken, serviceTypeCode string) (*Order, error) {
	if passengerID <= 0 || idempotencyKey == "" || len(idempotencyKey) > 128 || input.PassengerCount <= 0 || !validPhone(input.ContactPhone) || input.AgreementVersion == "" {
		return nil, fmt.Errorf("%w: missing order field", ErrInvalidArgument)
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
	input.Status = OrderStatusMatching
	input.ServiceType = option.ServiceType
	input.Origin, input.Destination = quote.Origin, quote.Destination
	input.DistanceMeters = quote.DistanceMeters
	input.EstimatedDurationSeconds = quote.EstimatedDurationSeconds
	input.Fare = option.Fare
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

func (uc *RideUsecase) ExecuteCommand(ctx context.Context, command *OrderCommand) (*OrderCommand, error) {
	if command.PassengerID <= 0 || command.OrderID <= 0 || command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return nil, ErrInvalidArgument
	}
	order, err := uc.repo.GetOrder(ctx, command.PassengerID, command.OrderID)
	if err != nil {
		return nil, err
	}
	next, err := commandTarget(order.Status, command.Type)
	if err != nil {
		return nil, err
	}
	if command.Type == "UNLOCK_AND_START" && command.BoardingCode == "" {
		return nil, fmt.Errorf("%w: boarding code is required", ErrInvalidArgument)
	}
	command.ResultStatus = next
	command.CurrentStatus = order.Status
	command.CommandNo = newPublicID("ocmd", uc.now())
	command.CreatedAt = uc.now()
	return uc.repo.ExecuteCommand(ctx, command)
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
	methodCode, ok := map[string]int16{"ALIPAY": 1, "WECHAT": 2, "BALANCE": 3, "MOCK": 4}[strings.ToUpper(method)]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported payment method", ErrInvalidArgument)
	}
	payment := &Payment{OrderID: order.ID, PassengerID: passengerID, IdempotencyKey: idempotencyKey, PaymentNo: newPublicID("pay", uc.now()), Method: methodCode, Amount: order.Fare.PayableCents}
	created, _, err := uc.repo.CreatePayment(ctx, payment)
	return created, err
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
	created, _, err := uc.repo.CreateReview(ctx, review)
	return created, err
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
