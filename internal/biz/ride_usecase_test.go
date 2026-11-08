package biz

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeRideRepo struct {
	serviceTypes  []ServiceType
	quote         *Quote
	orders        map[int64]*Order
	idempotency   map[string]*Order
	events        []OrderEvent
	payments      map[string]*Payment
	paymentKeys   map[string]*Payment
	reviews       map[int64]*Review
	cancellations map[int64]*Cancellation
	paid          map[int64]int64
	history       *HistoryPage
	liveView      *OrderLiveView
	settlement    *Settlement
	available     int32
	createCalls   int
}

func newFakeRepo() *fakeRideRepo {
	return &fakeRideRepo{
		orders:        map[int64]*Order{},
		idempotency:   map[string]*Order{},
		payments:      map[string]*Payment{},
		paymentKeys:   map[string]*Payment{},
		reviews:       map[int64]*Review{},
		cancellations: map[int64]*Cancellation{},
		paid:          map[int64]int64{},
		available:     3,
	}
}

func (r *fakeRideRepo) ResolveServiceArea(context.Context, string, Place, Place) (int64, error) {
	return 9, nil
}
func (r *fakeRideRepo) ListServiceTypes(context.Context, string, time.Time) ([]ServiceType, error) {
	return r.serviceTypes, nil
}
func (r *fakeRideRepo) CountAvailableVehicles(context.Context, int64, int64) (int32, error) {
	return r.available, nil
}
func (r *fakeRideRepo) FindCoupon(context.Context, int64, string, int64) (*Coupon, error) {
	return &Coupon{Name: "新客券", DiscountCents: 200, PassengerCoupon: 9}, nil
}
func (r *fakeRideRepo) CreateQuote(_ context.Context, quote *Quote) error {
	quote.ID = 1
	r.quote = quote
	return nil
}
func (r *fakeRideRepo) GetQuote(context.Context, string) (*Quote, error) { return r.quote, nil }
func (r *fakeRideRepo) CreateOrder(_ context.Context, order *Order) (*Order, bool, error) {
	key := order.IdempotencyKey
	if existing, ok := r.idempotency[key]; ok {
		return existing, true, nil
	}
	r.createCalls++
	order.ID, order.OrderNo = int64(10+r.createCalls), "AG10"
	r.orders[order.ID] = order
	r.idempotency[key] = order
	return order, false, nil
}
func (r *fakeRideRepo) FindOrderByIdempotency(_ context.Context, _ int64, key string) (*Order, error) {
	if existing, ok := r.idempotency[key]; ok {
		return existing, nil
	}
	return nil, ErrNotFound
}
func (r *fakeRideRepo) GetOrder(_ context.Context, passengerID, orderID int64) (*Order, error) {
	order, ok := r.orders[orderID]
	if !ok || order.PassengerID != passengerID {
		return nil, ErrNotFound
	}
	return order, nil
}

func (r *fakeRideRepo) GetOrderAny(_ context.Context, orderID int64) (*Order, error) {
	order, ok := r.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	return order, nil
}
func (r *fakeRideRepo) ListOrdersForAdmin(context.Context, AdminOrderFilter) (*AdminOrderPage, error) {
	return &AdminOrderPage{}, nil
}
func (r *fakeRideRepo) ListPassengersForAdmin(context.Context, AdminPassengerFilter) (*AdminPassengerPage, error) {
	return &AdminPassengerPage{}, nil
}
func (r *fakeRideRepo) GetPassengerAny(context.Context, int64) (*Passenger, error) {
	return &Passenger{ID: 1, Nickname: "测试乘客"}, nil
}
func (r *fakeRideRepo) ListVehiclesForAdmin(context.Context, AdminVehicleFilter) (*AdminVehiclePage, error) {
	return &AdminVehiclePage{}, nil
}
func (r *fakeRideRepo) GetVehicleAny(context.Context, int64) (*AdminVehicle, error) {
	return &AdminVehicle{ID: 3, PlateNo: "苏N·D1234"}, nil
}
func (r *fakeRideRepo) ListDispatchRecordsForAdmin(context.Context, AdminDispatchRecordFilter) (*AdminDispatchRecordPage, error) {
	return &AdminDispatchRecordPage{}, nil
}
func (r *fakeRideRepo) GetVehicle(context.Context, int64) (*Vehicle, error) {
	return &Vehicle{ID: 3, PlateNo: "苏N·D1234"}, nil
}
func (r *fakeRideRepo) PickAvailableVehicle(context.Context, int64) (*Vehicle, error) {
	return &Vehicle{ID: 3, PlateNo: "苏N·D1234"}, nil
}

func (r *fakeRideRepo) GetLiveView(context.Context, int64, int64) (*OrderLiveView, error) {
	return r.liveView, nil
}
func (r *fakeRideRepo) GetSettlement(context.Context, int64, int64) (*Settlement, error) {
	return r.settlement, nil
}
func (r *fakeRideRepo) ExecuteCommand(_ context.Context, command *OrderCommand) (*OrderCommand, error) {
	command.ID, command.Accepted = 11, true
	if order, ok := r.orders[command.OrderID]; ok {
		order.Status = command.ResultStatus
	}
	return command, nil
}
func (r *fakeRideRepo) CancelOrder(_ context.Context, cancellation *Cancellation) (*Cancellation, error) {
	if existing, ok := r.cancellations[cancellation.OrderID]; ok {
		return existing, nil
	}
	cancellation.Status = OrderStatusCancelled
	r.cancellations[cancellation.OrderID] = cancellation
	if order, ok := r.orders[cancellation.OrderID]; ok {
		order.Status = OrderStatusCancelled
	}
	return cancellation, nil
}
func (r *fakeRideRepo) PaidAmount(_ context.Context, orderID int64) (int64, error) {
	return r.paid[orderID], nil
}
func (r *fakeRideRepo) ListEvents(context.Context, int64, int64, int64) ([]OrderEvent, error) {
	return r.events, nil
}
func (r *fakeRideRepo) CreatePayment(_ context.Context, payment *Payment) (*Payment, bool, error) {
	key := payment.IdempotencyKey
	if existing, ok := r.paymentKeys[key]; ok {
		return existing, true, nil
	}
	payment.ID = 12
	r.paymentKeys[key] = payment
	r.payments[payment.PaymentNo] = payment
	return payment, false, nil
}
func (r *fakeRideRepo) HandlePaymentCallback(_ context.Context, callback *PaymentCallback) (*PaymentCallbackResult, error) {
	payment, ok := r.payments[callback.PaymentNo]
	if !ok {
		return nil, ErrNotFound
	}
	if callback.Result == "SUCCESS" {
		if payment.Status == PaymentStatusPaid {
			return &PaymentCallbackResult{Payment: payment, OrderID: payment.OrderID, Duplicated: true}, nil
		}
		payment.Status = PaymentStatusPaid
		payment.ChannelTradeNo = callback.ChannelTradeNo
		r.paid[payment.OrderID] += payment.Amount
	}
	return &PaymentCallbackResult{Payment: payment, OrderID: payment.OrderID}, nil
}
func (r *fakeRideRepo) CreateReview(_ context.Context, review *Review) (*Review, bool, error) {
	if existing, ok := r.reviews[review.OrderID]; ok {
		return existing, true, nil
	}
	review.ID = 13
	r.reviews[review.OrderID] = review
	return review, false, nil
}
func (r *fakeRideRepo) ListHistory(context.Context, int64, HistoryCursor, int, string) (*HistoryPage, error) {
	return r.history, nil
}

func newTestUsecase(repo RideRepo, now time.Time) *RideUsecase {
	uc := NewRideUsecase(repo, log.NewStdLogger(io.Discard))
	uc.now = func() time.Time { return now }
	return uc
}

func testServiceType() ServiceType {
	return ServiceType{
		ID: 1, Code: "rt6_saver", SeatCount: 4, PickupETASeconds: 180,
		Rule: PricingRule{ID: 2, Version: "v1", BaseFareCents: 1000,
			BaseDistanceMeters: 3000, DistanceUnitPriceCents: 100},
	}
}

func TestRideUsecaseEstimateFareCombinesCouponAndVehicles(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.serviceTypes = []ServiceType{testServiceType()}
	uc := newTestUsecase(repo, now)
	quote, err := uc.EstimateFare(context.Background(), &Quote{
		PassengerID: 7, CityCode: "420100", RideMode: "NOW",
		Origin:      Place{PlaceID: "o", Longitude: 114.1, Latitude: 30.1},
		Destination: Place{PlaceID: "d", Longitude: 114.2, Latitude: 30.2},
	}, "ucp_9")
	if err != nil {
		t.Fatal(err)
	}
	if quote.ID != 1 || quote.ServiceAreaID != 9 || len(quote.Options) != 1 {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	option := quote.Options[0]
	if option.AvailableVehicles != 3 {
		t.Fatalf("expected vehicle count 3, actual %d", option.AvailableVehicles)
	}
	if option.Fare.DiscountCents != 200 || quote.Coupon == nil {
		t.Fatalf("expected coupon applied, actual %+v", option.Fare)
	}
}

func TestRideUsecaseEstimateFareWithoutVehiclesExtendsETA(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.serviceTypes = []ServiceType{testServiceType()}
	repo.available = 0
	uc := newTestUsecase(repo, now)
	quote, err := uc.EstimateFare(context.Background(), &Quote{
		CityCode: "420100", RideMode: "NOW",
		Origin:      Place{PlaceID: "o", Longitude: 114.1, Latitude: 30.1},
		Destination: Place{PlaceID: "d", Longitude: 114.2, Latitude: 30.2},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Options[0].ServiceType.PickupETASeconds < 600 {
		t.Fatalf("expected ETA to be extended without vehicles, actual %d", quote.Options[0].ServiceType.PickupETASeconds)
	}
	if quote.Coupon != nil {
		t.Fatal("expected no coupon without passenger context")
	}
}

func TestRideUsecaseCreateOrderIsIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.serviceTypes = []ServiceType{testServiceType()}
	repo.quote = &Quote{
		ID: 1, Token: "qt_1", PassengerID: 7, ServiceAreaID: 9, RideMode: "NOW",
		DistanceMeters: 5000, EstimatedDurationSeconds: 600,
		Origin: Place{PlaceID: "o"}, Destination: Place{PlaceID: "d"},
		ExpiresAt: now.Add(time.Minute),
		Options:   []QuoteOption{{ServiceType: testServiceType(), Fare: Fare{OriginalCents: 2000, PayableCents: 2000}}},
	}
	uc := newTestUsecase(repo, now)
	first, err := uc.CreateOrder(context.Background(), 7, "order-1", &Order{
		PassengerCount: 1, ContactPhone: "13812345678", AgreementVersion: "v1",
	}, "qt_1", "rt6_saver")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != OrderStatusPendingDispatch {
		t.Fatalf("expected pending dispatch status, actual %d", first.Status)
	}
	// 重复提交时即使试算凭证已失效，也应返回首单（幂等优先）。
	second, err := uc.CreateOrder(context.Background(), 7, "order-1", &Order{
		PassengerCount: 1, ContactPhone: "13812345678", AgreementVersion: "v1",
	}, "qt_expired", "rt6_saver")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || repo.createCalls != 1 {
		t.Fatalf("expected single order for duplicated request, calls=%d", repo.createCalls)
	}
}

func TestRideUsecaseCreateOrderRejectsExpiredOrForeignQuote(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.quote = &Quote{ID: 1, PassengerID: 8, ExpiresAt: now.Add(-time.Minute)}
	uc := newTestUsecase(repo, now)
	_, err := uc.CreateOrder(context.Background(), 7, "order-1", &Order{
		PassengerCount: 1, ContactPhone: "13812345678", AgreementVersion: "v1",
	}, "qt_1", "rt6_saver")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, actual %v", err)
	}
}

func TestRideUsecaseGetOrderRejectsOtherPassenger(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7}
	uc := newTestUsecase(repo, now)
	if _, err := uc.GetOrder(context.Background(), 7, 10); err != nil {
		t.Fatalf("owner should read own order: %v", err)
	}
	if _, err := uc.GetOrder(context.Background(), 8, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found for other passenger, actual %v", err)
	}
}

func TestRideUsecaseCancelOrderChargesFeeAndRefunds(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusPickingUp,
		Fare: Fare{PayableCents: 2000}}
	repo.paid[10] = 2000
	uc := newTestUsecase(repo, now)
	cancellation, err := uc.CancelOrder(context.Background(), 7, 10, "wait too long")
	if err != nil {
		t.Fatal(err)
	}
	if cancellation.FeeCents != 500 || cancellation.RefundCents != 1500 {
		t.Fatalf("unexpected cancellation: %+v", cancellation)
	}
	if cancellation.RuleCode != CancelRulePickingUpFee || cancellation.ReasonCode != "WAIT TOO LONG" {
		t.Fatalf("unexpected rule or reason: %+v", cancellation)
	}
	if repo.orders[10].Status != OrderStatusCancelled {
		t.Fatal("expected order cancelled")
	}
}

func TestRideUsecaseCancelOrderIsIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusPendingDispatch,
		Fare: Fare{PayableCents: 2000}}
	uc := newTestUsecase(repo, now)
	first, err := uc.CancelOrder(context.Background(), 7, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.CancelOrder(context.Background(), 7, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.FeeCents != second.FeeCents || first.CancelledAt != second.CancelledAt {
		t.Fatalf("expected duplicated cancel to reuse result: %+v / %+v", first, second)
	}
}

func TestRideUsecaseCancelOrderRejectsInTrip(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusInTrip}
	uc := newTestUsecase(repo, now)
	if _, err := uc.CancelOrder(context.Background(), 7, 10, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, actual %v", err)
	}
}

func TestRideUsecaseCommandFlow(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusWaitingBoarding}
	uc := newTestUsecase(repo, now)
	command, err := uc.ExecuteCommand(context.Background(), &OrderCommand{
		OrderID: 10, PassengerID: 7, Type: CommandUnlockAndStart,
		IdempotencyKey: "command-1", BoardingCode: "1234",
	})
	if err != nil || command.ResultStatus != OrderStatusInTrip || !command.Accepted {
		t.Fatalf("unexpected command: %+v, %v", command, err)
	}
	if _, err := uc.ExecuteCommand(context.Background(), &OrderCommand{
		OrderID: 10, PassengerID: 7, Type: "HACK", IdempotencyKey: "command-2",
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid command, actual %v", err)
	}
}

func TestRideUsecasePaymentAndCallbackIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusCompleted,
		Fare: Fare{PayableCents: 1880}}
	uc := newTestUsecase(repo, now)
	payment, err := uc.CreatePayment(context.Background(), 7, 10, "WECHAT", "payment-1")
	if err != nil || payment.Amount != 1880 || payment.Method != 2 {
		t.Fatalf("unexpected payment: %+v, %v", payment, err)
	}
	first, err := uc.HandlePaymentCallback(context.Background(), &PaymentCallback{
		PaymentNo: payment.PaymentNo, ChannelTradeNo: "wx-1", Result: "success", AmountCents: 1880,
	})
	if err != nil || first.Duplicated || first.Payment.Status != PaymentStatusPaid {
		t.Fatalf("unexpected callback: %+v, %v", first, err)
	}
	second, err := uc.HandlePaymentCallback(context.Background(), &PaymentCallback{
		PaymentNo: payment.PaymentNo, ChannelTradeNo: "wx-1", Result: "SUCCESS", AmountCents: 1880,
	})
	if err != nil || !second.Duplicated {
		t.Fatalf("expected duplicated callback, actual %+v, %v", second, err)
	}
	if repo.paid[10] != 1880 {
		t.Fatalf("expected single payment record, actual %d", repo.paid[10])
	}
}

func TestRideUsecasePaymentCallbackValidation(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	uc := newTestUsecase(newFakeRepo(), now)
	if _, err := uc.HandlePaymentCallback(context.Background(), &PaymentCallback{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected missing payment no error, actual %v", err)
	}
	if _, err := uc.HandlePaymentCallback(context.Background(), &PaymentCallback{
		PaymentNo: "pay_1", Result: "SUCCESS",
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected missing amount error, actual %v", err)
	}
}

func TestRideUsecaseReviewRules(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusCompleted}
	uc := newTestUsecase(repo, now)
	review, err := uc.SubmitReview(context.Background(), &Review{
		OrderID: 10, PassengerID: 7, Rating: 5, Tags: []string{"CLEAN"}, Anonymous: true,
	})
	if err != nil || review.ID != 13 || !review.Anonymous {
		t.Fatalf("unexpected review: %+v, %v", review, err)
	}
	if _, err := uc.SubmitReview(context.Background(), &Review{
		OrderID: 10, PassengerID: 7, Rating: 4,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicated review conflict, actual %v", err)
	}
	if _, err := uc.SubmitReview(context.Background(), &Review{
		OrderID: 10, PassengerID: 7, Rating: 9,
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid rating, actual %v", err)
	}
}

func TestRideUsecaseSettlementRequiresTerminalOrder(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.settlement = &Settlement{Order: &Order{ID: 10, Status: OrderStatusInTrip}}
	uc := newTestUsecase(repo, now)
	if _, err := uc.GetSettlement(context.Background(), 7, 10); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict for unsettled order, actual %v", err)
	}
	repo.settlement.Order.Status = OrderStatusCompleted
	if _, err := uc.GetSettlement(context.Background(), 7, 10); err != nil {
		t.Fatalf("unexpected settlement error: %v", err)
	}
}

func TestRideUsecaseHistoryAndEvents(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7}
	repo.events = []OrderEvent{{ID: 1, Status: OrderStatusPendingDispatch}}
	repo.history = &HistoryPage{Items: []*Order{{ID: 10}}}
	uc := newTestUsecase(repo, now)
	events, err := uc.ListEvents(context.Background(), 7, 10, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("unexpected events: %+v, %v", events, err)
	}
	page, err := uc.ListHistory(context.Background(), 7, "", 20, "ALL")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("unexpected history: %+v, %v", page, err)
	}
	if _, err := uc.ListHistory(context.Background(), 7, "", 100, "ALL"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected page size validation, actual %v", err)
	}
	if _, err := uc.ListHistory(context.Background(), 7, "", 20, "UNKNOWN"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected status group validation, actual %v", err)
	}
}

func TestRideUsecaseAdminDispatch(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	repo.orders[10] = &Order{ID: 10, PassengerID: 7, Status: OrderStatusPendingDispatch}
	uc := newTestUsecase(repo, now)
	order, vehicle, err := uc.AdminDispatch(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != OrderStatusPickingUp {
		t.Fatalf("expected picking up after dispatch, actual %d", order.Status)
	}
	if vehicle == nil || vehicle.PlateNo == "" {
		t.Fatalf("expected vehicle assigned, actual %+v", vehicle)
	}
	if _, _, err := uc.AdminDispatch(context.Background(), 10, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict when dispatching a picking-up order, actual %v", err)
	}
}
