package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("business conflict")
)

// OrderStatus 与《萝卜快跑开发任务》4.3 的订单状态机一致：
// 0 待派车、1 派车中、2 接驾中、3 待上车、4 行程中、5 已完成、6 已取消、7 异常关闭。
type OrderStatus int16

const (
	OrderStatusPendingDispatch OrderStatus = iota // 0 待派车
	OrderStatusDispatching                        // 1 派车中
	OrderStatusPickingUp                          // 2 接驾中
	OrderStatusWaitingBoarding                    // 3 待上车
	OrderStatusInTrip                             // 4 行程中
	OrderStatusCompleted                          // 5 已完成
	OrderStatusCancelled                          // 6 已取消
	OrderStatusExceptionClosed                    // 7 异常关闭
)

// PaymentStatus 与 payments.payment_status 枚举一致。
const (
	PaymentStatusPending int16 = iota
	PaymentStatusProcessing
	PaymentStatusPaid
	PaymentStatusFailed
	PaymentStatusClosed
	PaymentStatusPartiallyRefunded
	PaymentStatusRefunded
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

type QuoteCoupon struct {
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
	ServiceType       ServiceType
	Fare              Fare
	AvailableVehicles int32
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
	Coupon                   *QuoteCoupon
	Options                  []QuoteOption
	ExpiresAt                time.Time
}

type RideVehicle struct {
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
	Vehicle                  *RideVehicle
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
	VehicleID       int64
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
	ChannelTradeNo string
	PaidAt         *time.Time
}

type PaymentCallback struct {
	PaymentNo      string
	ChannelTradeNo string
	Result         string
	AmountCents    int64
	OccurredAt     time.Time
}

type PaymentCallbackResult struct {
	Payment    *Payment
	OrderID    int64
	Duplicated bool
}

type Refund struct {
	RefundNo    string
	PaymentID   int64
	OrderID     int64
	PassengerID int64
	AmountCents int64
	Reason      string
	CreatedAt   time.Time
}

type Review struct {
	ID          int64
	OrderID     int64
	PassengerID int64
	Rating      int32
	Tags        []string
	Comment     string
	Anonymous   bool
	CreatedAt   time.Time
}

type Cancellation struct {
	OrderID     int64
	PassengerID int64
	ReasonCode  string
	RuleCode    string
	FeeCents    int64
	RefundCents int64
	PaidCents   int64
	CancelledAt time.Time
	Status      OrderStatus
}

type LocationPoint struct {
	Longitude                float64
	Latitude                 float64
	SpeedKMH                 float64
	ReportedAt               time.Time
	ProgressPercent          int32
	RemainingDistanceMeters  int32
	RemainingDurationSeconds int32
}

type DispatchSummary struct {
	Strategy             string
	StrategyVersion      string
	Status               string
	VehicleResponse      string
	AttemptCount         int32
	PickupETASeconds     int32
	PickupDistanceMeters float64
	DispatchedAt         *time.Time
}

type BoardingVerification struct {
	Method     string
	Status     string
	VerifiedAt *time.Time
	Attempts   int32
}

type SafetyEvent struct {
	ID          int64
	Type        string
	Level       string
	Description string
	Status      string
	OccurredAt  time.Time
}

type TripProgress struct {
	RemainingDistanceMeters  int32
	RemainingDurationSeconds int32
	SpeedKMH                 float64
	ProgressPercent          int32
}

// OrderLiveView 是活动订单聚合详情，一次返回订单、车辆、位置、派单、核验和安全事件摘要。
type OrderLiveView struct {
	Order    *Order
	Location *LocationPoint
	Dispatch *DispatchSummary
	Boarding *BoardingVerification
	Safety   []SafetyEvent
}

type SettlementFeeItem struct {
	Type        string
	Name        string
	AmountCents int64
}

type SettlementPayment struct {
	PaymentID   int64
	PaymentNo   string
	Method      string
	Status      string
	AmountCents int64
	PaidAt      *time.Time
}

type SettlementReview struct {
	Submitted   bool
	Rating      int32
	Tags        []string
	SubmittedAt *time.Time
}

type Settlement struct {
	Order         *Order
	FeeItems      []SettlementFeeItem
	Payment       *SettlementPayment
	PaidCents     int64
	RefundedCents int64
	Review        *SettlementReview
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

// AdminOrderFilter 后台订单列表筛选条件。
type AdminOrderFilter struct {
	Status   *int16
	Keyword  string
	Page     int
	PageSize int
}

type AdminOrderPage struct {
	Items []*Order
	Total int64
}

// Passenger 后台视角的乘客档案。
type AdminPassenger struct {
	ID               int64
	PassengerNo      string
	Mobile           string
	Nickname         string
	AvatarURL        string
	ExternalProvider string
	RealNameStatus   int16
	AccountStatus    int16
	PointBalance     int32
	LastLoginAt      *time.Time
	CreatedAt        time.Time
}

// AdminPassengerFilter 后台乘客列表筛选条件。
type AdminPassengerFilter struct {
	Keyword        string
	RealNameStatus *int16
	AccountStatus  *int16
	Page           int
	PageSize       int
}

type AdminPassengerPage struct {
	Items []*AdminPassenger
	Total int64
}

// AdminVehicle 后台视角的车辆档案。
type AdminVehicle struct {
	ID              int64
	VehicleNo       string
	VIN             string
	PlateNo         string
	Model           string
	SeatCount       int16
	ServiceAreaID   *int64
	ServiceAreaName string
	OperationStatus int16
	OnlineStatus    int16
	BatteryPercent  *float64
	SoftwareVersion string
	LastReportedAt  *time.Time
	CreatedAt       time.Time
}

// AdminVehicleFilter 后台车辆列表筛选条件。
type AdminVehicleFilter struct {
	Keyword         string
	OperationStatus *int16
	OnlineStatus    *int16
	Page            int
	PageSize        int
}

type AdminVehiclePage struct {
	Items []*AdminVehicle
	Total int64
}

// AdminDispatchRecord 后台视角的派单记录。
type AdminDispatchRecord struct {
	ID                        int64
	DispatchNo                string
	OrderID                   int64
	OrderNo                   string
	VehicleID                 *int64
	PlateNo                   string
	DispatchStrategy          string
	CandidateScore            *float64
	EstimatedPickupDistanceKM *float64
	EstimatedArrivalMin       *int32
	Status                    int16
	FailureReason             string
	DispatchedAt              *time.Time
	RespondedAt               *time.Time
	CreatedAt                 time.Time
}

// AdminDispatchRecordFilter 后台派单记录筛选条件。
type AdminDispatchRecordFilter struct {
	Keyword  string
	Status   *int16
	Page     int
	PageSize int
}

type AdminDispatchRecordPage struct {
	Items []*AdminDispatchRecord
	Total int64
}

type RideRepo interface {
	ResolveServiceArea(ctx context.Context, cityCode string, origin, destination Place) (int64, error)
	ListServiceTypes(ctx context.Context, cityCode string, now time.Time) ([]ServiceType, error)
	CountAvailableVehicles(ctx context.Context, serviceAreaID, serviceTypeID int64) (int32, error)
	FindCoupon(ctx context.Context, passengerID int64, couponID string, originalCents int64) (*QuoteCoupon, error)
	CreateQuote(ctx context.Context, quote *Quote) error
	GetQuote(ctx context.Context, token string) (*Quote, error)
	CreateOrder(ctx context.Context, order *Order) (*Order, bool, error)
	FindOrderByIdempotency(ctx context.Context, passengerID int64, idempotencyKey string) (*Order, error)
	GetOrder(ctx context.Context, passengerID, orderID int64) (*Order, error)
	GetOrderAny(ctx context.Context, orderID int64) (*Order, error)
	ListOrdersForAdmin(ctx context.Context, filter AdminOrderFilter) (*AdminOrderPage, error)
	ListPassengersForAdmin(ctx context.Context, filter AdminPassengerFilter) (*AdminPassengerPage, error)
	GetPassengerAny(ctx context.Context, passengerID int64) (*AdminPassenger, error)
	ListVehiclesForAdmin(ctx context.Context, filter AdminVehicleFilter) (*AdminVehiclePage, error)
	GetVehicleAny(ctx context.Context, vehicleID int64) (*AdminVehicle, error)
	ListDispatchRecordsForAdmin(ctx context.Context, filter AdminDispatchRecordFilter) (*AdminDispatchRecordPage, error)
	GetVehicle(ctx context.Context, vehicleID int64) (*RideVehicle, error)
	PickAvailableVehicle(ctx context.Context, serviceAreaID int64) (*RideVehicle, error)
	GetLiveView(ctx context.Context, passengerID, orderID int64) (*OrderLiveView, error)
	GetSettlement(ctx context.Context, passengerID, orderID int64) (*Settlement, error)
	ExecuteCommand(ctx context.Context, command *OrderCommand) (*OrderCommand, error)
	CancelOrder(ctx context.Context, cancellation *Cancellation) (*Cancellation, error)
	PaidAmount(ctx context.Context, orderID int64) (int64, error)
	ListEvents(ctx context.Context, passengerID, orderID, afterID int64) ([]OrderEvent, error)
	CreatePayment(ctx context.Context, payment *Payment) (*Payment, bool, error)
	HandlePaymentCallback(ctx context.Context, callback *PaymentCallback) (*PaymentCallbackResult, error)
	CreateReview(ctx context.Context, review *Review) (*Review, bool, error)
	ListHistory(ctx context.Context, passengerID int64, cursor HistoryCursor, pageSize int, statusGroup string) (*HistoryPage, error)
}

type RideUsecase struct {
	repo RideRepo
	log  *log.Helper
	now  func() time.Time
}

func NewRideUsecase(repo RideRepo, logger log.Logger) *RideUsecase {
	return &RideUsecase{repo: repo, log: log.NewHelper(logger), now: time.Now}
}
