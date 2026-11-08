package data

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"
)

type cents int64

func (c *cents) Scan(value any) error {
	var text string
	switch typed := value.(type) {
	case nil:
		*c = 0
		return nil
	case []byte:
		text = string(typed)
	case string:
		text = typed
	case float64:
		*c = cents(typed*100 + 0.5)
		return nil
	default:
		text = fmt.Sprint(value)
	}
	parts := strings.SplitN(text, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return err
	}
	fraction := "00"
	if len(parts) == 2 {
		fraction = (parts[1] + "00")[:2]
	}
	minor, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return err
	}
	if whole < 0 {
		minor = -minor
	}
	*c = cents(whole*100 + minor)
	return nil
}

func (c cents) Value() (driver.Value, error) {
	value := int64(c)
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100), nil
}

type serviceTypePricingRow struct {
	ID                     int64
	ServiceTypeCode        string
	DisplayName            string
	Tags                   []byte
	SeatCount              int32
	PickupETASeconds       int32
	PricingRuleID          int64
	Version                string
	BaseFare               cents
	BaseDistanceM          int32
	DistanceUnitPriceCents int64
}

type quoteRow struct {
	ID                       int64 `gorm:"primaryKey"`
	QuoteToken               string
	PassengerID              *int64
	ServiceAreaID            int64
	CityCode                 string
	RideMode                 string
	ScheduledAt              *time.Time
	OriginPlaceID            string
	OriginName               string
	OriginAddress            string
	OriginLongitude          float64
	OriginLatitude           float64
	DestinationPlaceID       string
	DestinationName          string
	DestinationAddress       string
	DestinationLongitude     float64
	DestinationLatitude      float64
	DistanceMeters           int32
	EstimatedDurationSeconds int32
	Polyline                 string
	PassengerCouponID        *int64
	ExpiresAt                time.Time
	CreatedAt                time.Time
}

func (quoteRow) TableName() string { return "fare_quote_snapshots" }

type quoteOptionRow struct {
	ID               int64 `gorm:"primaryKey"`
	QuoteID          int64
	ServiceTypeID    int64
	PricingRuleID    int64
	PickupETASeconds int32
	OriginalAmount   cents
	DiscountAmount   cents
	PayableAmount    cents
	CreatedAt        time.Time
}

func (quoteOptionRow) TableName() string { return "fare_quote_options" }

type OrderRow struct {
	ID                     int64 `gorm:"primaryKey"`
	OrderNo                string
	PassengerID            int64
	VehicleID              *int64
	ServiceAreaID          int64
	OrderStatus            int16 `gorm:"column:status"`
	PickupStationID        int64
	DropoffStationID       int64
	PickupSnapshot         []byte `gorm:"type:jsonb"`
	DropoffSnapshot        []byte `gorm:"type:jsonb"`
	PricingRuleID          *int64
	PickupName             string
	PickupAddress          string
	PickupLongitude        float64
	PickupLatitude         float64
	DestinationName        string
	DestinationAddress     string
	DestinationLongitude   float64
	DestinationLatitude    float64
	EstimatedDistanceM     int32
	EstimatedDurationS     int32
	ActualDistanceM        int32
	ActualDurationS        int32
	EstimatedAmount        cents
	OriginalAmount         cents
	DiscountAmount         cents
	PayableAmount          cents
	PassengerCouponID      *int64
	RequestedAt            time.Time
	ArrivedAt              *time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	CancelledAt            *time.Time
	QuoteID                int64
	ServiceTypeID          int64
	IdempotencyKey         string
	RideMode               string
	ScheduledAt            *time.Time
	PassengerCount         int32
	ContactPhoneCiphertext []byte
	ContactPhoneMasked     string
	UserAgreementVersion   string
	PricingRuleVersion     string
	BoardingCodeHash       string
	BoardingCodeSuffix     string
	CreatedAt              time.Time
}

func (OrderRow) TableName() string { return "trip_orders" }

type orderDetailRow struct {
	OrderRow
	ServiceTypeCode    string
	ServiceDisplayName string
	PlateNo            string
	VehicleNo          string
	VehicleModel       string
	VehicleColor       string
	PaymentStatus      *int16
	LastEventID        int64
}

type commandRow struct {
	ID              int64 `gorm:"primaryKey"`
	CommandNo       string
	OrderID         int64
	PassengerID     int64
	CommandType     string
	IdempotencyKey  string
	Status          int16
	ResultCode      string
	ReasonCode      string
	ClientLongitude float64
	ClientLatitude  float64
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

func (commandRow) TableName() string { return "order_commands" }

type paymentRow struct {
	ID              int64 `gorm:"primaryKey"`
	PaymentNo       string
	OrderID         int64
	PassengerID     int64
	PaymentMethod   int16
	Amount          cents
	PaymentStatus   int16
	IdempotencyKey  string
	ProviderTradeNo string `gorm:"column:provider_trade_no"`
	RefundedAmount  cents
	PaidAt          *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (paymentRow) TableName() string { return "payments" }

type reviewRow struct {
	ID           int64 `gorm:"primaryKey"`
	OrderID      int64
	PassengerID  int64
	OverallScore int32
	SafetyScore  *int32
	ComfortScore *int32
	Comment      string `gorm:"column:review_content"`
	Tags         []byte `gorm:"type:jsonb"`
	Anonymous    bool   `gorm:"column:is_anonymous"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (reviewRow) TableName() string { return "trip_reviews" }

func encryptPhone(key, value string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("contact phone encryption key is required")
	}
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(value), nil), nil
}

func maskPhone(value string) string {
	runes := []rune(value)
	if len(runes) < 7 {
		return "****"
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

func newOrderNo(now time.Time) (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("AG%s%06d", now.Format("20060102150405"), number.Int64()), nil
}

// newPublicID 生成对外可见的业务编号，包含时间戳与随机后缀避免碰撞。
func newPublicID(prefix string, now time.Time) string {
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Sprintf("%s%d", prefix, now.UnixNano())
	}
	return fmt.Sprintf("%s%s%06d", prefix, now.Format("20060102150405"), number.Int64())
}

func newBoardingCode() (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", number.Int64()), nil
}

func hashSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func decodeTags(value []byte) []string {
	var tags []string
	_ = json.Unmarshal(value, &tags)
	return tags
}

func encodeTags(value []string) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func parsePublicID(value string) (int64, error) {
	parts := strings.Split(value, "_")
	for index := len(parts) - 1; index >= 0; index-- {
		if id, err := strconv.ParseInt(parts[index], 10, 64); err == nil && id > 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("invalid public id")
}
