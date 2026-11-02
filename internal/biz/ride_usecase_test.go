package biz

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeRideRepo struct {
	serviceTypes []ServiceType
	quote        *Quote
	order        *Order
	events       []OrderEvent
	payment      *Payment
	review       *Review
	history      *HistoryPage
}

func (r *fakeRideRepo) ListServiceTypes(context.Context, string, time.Time) ([]ServiceType, error) {
	return r.serviceTypes, nil
}
func (r *fakeRideRepo) FindCoupon(context.Context, int64, string, int64) (*Coupon, error) {
	return &Coupon{DiscountCents: 200, PassengerCoupon: 9}, nil
}
func (r *fakeRideRepo) CreateQuote(_ context.Context, quote *Quote) error {
	quote.ID = 1
	r.quote = quote
	return nil
}
func (r *fakeRideRepo) GetQuote(context.Context, string) (*Quote, error) { return r.quote, nil }
func (r *fakeRideRepo) CreateOrder(_ context.Context, order *Order) (*Order, bool, error) {
	order.ID, order.OrderNo = 10, "AG10"
	r.order = order
	return order, false, nil
}
func (r *fakeRideRepo) GetOrder(context.Context, int64, int64) (*Order, error) {
	return r.order, nil
}
func (r *fakeRideRepo) ExecuteCommand(_ context.Context, command *OrderCommand) (*OrderCommand, error) {
	command.ID, command.Accepted = 11, true
	return command, nil
}
func (r *fakeRideRepo) ListEvents(context.Context, int64, int64, int64) ([]OrderEvent, error) {
	return r.events, nil
}
func (r *fakeRideRepo) CreatePayment(_ context.Context, payment *Payment) (*Payment, bool, error) {
	payment.ID = 12
	r.payment = payment
	return payment, false, nil
}
func (r *fakeRideRepo) CreateReview(_ context.Context, review *Review) (*Review, bool, error) {
	review.ID = 13
	r.review = review
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

func TestRideUsecaseOrderFlow(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	serviceType := ServiceType{
		ID: 1, Code: "rt6_saver", SeatCount: 4,
		Rule: PricingRule{ID: 2, Version: "v1", BaseFareCents: 1000,
			BaseDistanceMeters: 3000, DistanceUnitPriceCents: 100},
	}
	repo := &fakeRideRepo{serviceTypes: []ServiceType{serviceType}}
	uc := newTestUsecase(repo, now)
	quote, err := uc.EstimateFare(context.Background(), &Quote{
		PassengerID: 7, CityCode: "420100", RideMode: "NOW",
		Origin:      Place{PlaceID: "o", Longitude: 114.1, Latitude: 30.1},
		Destination: Place{PlaceID: "d", Longitude: 114.2, Latitude: 30.2},
	}, "ucp_9")
	if err != nil || quote.ID != 1 || len(quote.Options) != 1 {
		t.Fatalf("unexpected quote: %+v, %v", quote, err)
	}
	order, err := uc.CreateOrder(context.Background(), 7, "create-1", &Order{
		PassengerCount: 2, ContactPhone: "13812345678", AgreementVersion: "v1",
	}, quote.Token, "rt6_saver")
	if err != nil || order.ID != 10 || order.PassengerCouponID != 9 {
		t.Fatalf("unexpected order: %+v, %v", order, err)
	}
}

func TestRideUsecaseCommandPaymentAndReview(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	repo := &fakeRideRepo{order: &Order{ID: 10, PassengerID: 7, Status: OrderStatusArrived}}
	uc := newTestUsecase(repo, now)
	command, err := uc.ExecuteCommand(context.Background(), &OrderCommand{
		OrderID: 10, PassengerID: 7, Type: "UNLOCK_AND_START",
		IdempotencyKey: "command-1", BoardingCode: "1234",
	})
	if err != nil || command.ResultStatus != OrderStatusInTrip || !command.Accepted {
		t.Fatalf("unexpected command: %+v, %v", command, err)
	}
	repo.order.Status, repo.order.Fare.PayableCents = OrderStatusCompleted, 1880
	payment, err := uc.CreatePayment(context.Background(), 7, 10, "WECHAT", "payment-1")
	if err != nil || payment.Amount != 1880 || payment.Method != 2 {
		t.Fatalf("unexpected payment: %+v, %v", payment, err)
	}
	review, err := uc.SubmitReview(context.Background(), &Review{
		OrderID: 10, PassengerID: 7, Rating: 5, Tags: []string{"CLEAN"},
	})
	if err != nil || review.ID != 13 || !review.CreatedAt.Equal(now) {
		t.Fatalf("unexpected review: %+v, %v", review, err)
	}
}

func TestRideUsecaseHistoryAndEvents(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	repo := &fakeRideRepo{
		order:   &Order{ID: 10, PassengerID: 7},
		events:  []OrderEvent{{ID: 1, Status: OrderStatusMatching}},
		history: &HistoryPage{Items: []*Order{{ID: 10}}, HasMore: false},
	}
	uc := newTestUsecase(repo, now)
	events, err := uc.ListEvents(context.Background(), 7, 10, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("unexpected events: %+v, %v", events, err)
	}
	page, err := uc.ListHistory(context.Background(), 7, "", 20, "ALL")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("unexpected history: %+v, %v", page, err)
	}
}
