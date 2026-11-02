package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("resource not found")
	ErrConflict        = errors.New("business conflict")
)

type OrderStatus int16

const (
	OrderStatusMatching OrderStatus = iota
	OrderStatusPickingUp
	OrderStatusArrived
	OrderStatusInTrip
	OrderStatusCompleted
	OrderStatusCancelled
	OrderStatusClosed
)

type Place struct {
	PlaceID   string
	Name      string
	Address   string
	Longitude float64
	Latitude  float64
}

type FareItem struct {
	Type        string
	Name        string
	AmountCents int64
}

type Fare struct {
	OriginalCents int64
	DiscountCents int64
	PayableCents  int64
	Items         []FareItem
	PaymentStatus string
}

type Coupon struct {
	ID              int64
	Name            string
	DiscountCents   int64
	ValidUntil      time.Time
	PassengerCoupon int64
}

type PricingRule struct {
	ID                     int64
	Version                string
	BaseFareCents          int64
	BaseDistanceMeters     int32
	DistanceUnitPriceCents int64
}

type ServiceType struct {
	ID               int64
	Code             string
	DisplayName      string
	Tags             []string
	SeatCount        int32
	PickupETASeconds int32
	Rule             PricingRule
}

type QuoteOption struct {
	ServiceType ServiceType
	Fare        Fare
}

type Quote struct {
	ID                       int64
	Token                    string
	PassengerID              int64
	ServiceAreaID            int64
	CityCode                 string
	RideMode                 string
	ScheduledAt              *time.Time
	Origin                   Place
	Destination              Place
	DistanceMeters           int32
	EstimatedDurationSeconds int32
	Polyline                 string
	Coupon                   *Coupon
	Options                  []QuoteOption
	ExpiresAt                time.Time
}

type Vehicle struct {
	ID              int64
	PlateNo         string
	Model           string
	BodyNo          string
	Color           string
	ServiceTypeName string
}

type Order struct {
	ID                       int64
	OrderNo                  string
	PassengerID              int64
	ServiceAreaID            int64
	Vehicle                  *Vehicle
	Status                   OrderStatus
	ServiceType              ServiceType
	Origin                   Place
	Destination              Place
	DistanceMeters           int32
	EstimatedDurationSeconds int32
	ActualDistanceMeters     int32
	ActualDurationSeconds    int32
	Fare                     Fare
	RequestedAt              time.Time
	ArrivedAt                *time.Time
	StartedAt                *time.Time
	CompletedAt              *time.Time
	CancelledAt              *time.Time
	LastEventID              int64
	IdempotencyKey           string
	PassengerCount           int32
	ContactPhone             string
	AgreementVersion         string
	QuoteID                  int64
	PassengerCouponID        int64
	PricingRuleVersion       string
	BoardingCodeSuffix       string
	RideMode                 string
	ScheduledAt              *time.Time
}

type OrderEvent struct {
	ID         int64
	EventType  string
	OccurredAt time.Time
	Status     OrderStatus
	Longitude  float64
	Latitude   float64
	SpeedKMH   float64
}

type OrderCommand struct {
	ID              int64
	CommandNo       string
	OrderID         int64
	PassengerID     int64
	Type            string
	IdempotencyKey  string
	BoardingCode    string
	ReasonCode      string
	ClientLongitude float64
	ClientLatitude  float64
	Accepted        bool
	ResultStatus    OrderStatus
	CreatedAt       time.Time
	CurrentStatus   OrderStatus
}

type Payment struct {
	ID             int64
	PaymentNo      string
	OrderID        int64
	PassengerID    int64
	Method         int16
	Amount         int64
	Status         int16
	IdempotencyKey string
}

type Review struct {
	ID          int64
	OrderID     int64
	PassengerID int64
	Rating      int32
	Tags        []string
	Comment     string
	CreatedAt   time.Time
}

type HistoryCursor struct {
	CreatedAt time.Time
	ID        int64
}

type HistoryPage struct {
	Items   []*Order
	Next    *HistoryCursor
	HasMore bool
}

type RideRepo interface {
	ListServiceTypes(context.Context, string, time.Time) ([]ServiceType, error)
	FindCoupon(context.Context, int64, string, int64) (*Coupon, error)
	CreateQuote(context.Context, *Quote) error
	GetQuote(context.Context, string) (*Quote, error)
	CreateOrder(context.Context, *Order) (*Order, bool, error)
	GetOrder(context.Context, int64, int64) (*Order, error)
	ExecuteCommand(context.Context, *OrderCommand) (*OrderCommand, error)
	ListEvents(context.Context, int64, int64, int64) ([]OrderEvent, error)
	CreatePayment(context.Context, *Payment) (*Payment, bool, error)
	CreateReview(context.Context, *Review) (*Review, bool, error)
	ListHistory(context.Context, int64, HistoryCursor, int, string) (*HistoryPage, error)
}

type RideUsecase struct {
	repo RideRepo
	log  *log.Helper
	now  func() time.Time
}

func NewRideUsecase(repo RideRepo, logger log.Logger) *RideUsecase {
	return &RideUsecase{repo: repo, log: log.NewHelper(logger), now: time.Now}
}
