package biz

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func validateQuote(quote *Quote, now time.Time) error {
	if quote == nil || quote.CityCode == "" || quote.Origin.PlaceID == "" || quote.Destination.PlaceID == "" {
		return fmt.Errorf("%w: missing quote field", ErrInvalidArgument)
	}
	if !validCoordinate(quote.Origin) || !validCoordinate(quote.Destination) {
		return fmt.Errorf("%w: invalid coordinate", ErrInvalidArgument)
	}
	if quote.RideMode != "NOW" && quote.RideMode != "SCHEDULED" {
		return fmt.Errorf("%w: invalid ride mode", ErrInvalidArgument)
	}
	if quote.RideMode == "SCHEDULED" && (quote.ScheduledAt == nil || !quote.ScheduledAt.After(now)) {
		return fmt.Errorf("%w: scheduled time must be in the future", ErrInvalidArgument)
	}
	return nil
}

func validCoordinate(place Place) bool {
	return place.Longitude >= -180 && place.Longitude <= 180 && place.Latitude >= -90 && place.Latitude <= 90
}

func validPhone(value string) bool {
	if len(value) != 11 || value[0] != '1' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func estimateRoute(origin, destination Place) (int32, int32) {
	const earthRadius = 6371000
	lat1, lat2 := origin.Latitude*math.Pi/180, destination.Latitude*math.Pi/180
	deltaLat := (destination.Latitude - origin.Latitude) * math.Pi / 180
	deltaLon := (destination.Longitude - origin.Longitude) * math.Pi / 180
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	distance := 2 * earthRadius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a)) * 1.2
	duration := distance / (35 * 1000 / 3600)
	return int32(math.Round(distance)), int32(math.Round(duration))
}

func calculateFare(rule PricingRule, distance int32, coupon *QuoteCoupon) Fare {
	extraMeters := int64(max(0, distance-rule.BaseDistanceMeters))
	distanceCents := int64(math.Ceil(float64(extraMeters)/1000)) * rule.DistanceUnitPriceCents
	original := rule.BaseFareCents + distanceCents
	discount := int64(0)
	if coupon != nil {
		discount = min(coupon.DiscountCents, original)
	}
	items := []FareItem{
		{Type: "BASE_FARE", Name: "起步价", AmountCents: rule.BaseFareCents},
		{Type: "DISTANCE_FARE", Name: "里程费", AmountCents: distanceCents},
	}
	if discount > 0 {
		items = append(items, FareItem{Type: "COUPON", Name: "优惠券", AmountCents: -discount})
	}
	return Fare{
		OriginalCents: original,
		DiscountCents: discount,
		PayableCents:  original - discount,
		Items:         items,
	}
}

func findQuoteOption(options []QuoteOption, code string) (QuoteOption, bool) {
	for _, option := range options {
		if option.ServiceType.Code == code {
			return option, true
		}
	}
	return QuoteOption{}, false
}

func newPublicID(prefix string, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", prefix, now.UnixNano())))
	return fmt.Sprintf("%s_%d_%x", prefix, now.Unix(), sum[:4])
}

func EncodeHistoryCursor(cursor HistoryCursor) string {
	if cursor.ID == 0 {
		return ""
	}
	raw := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(cursor.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeHistoryCursor(value string) (HistoryCursor, error) {
	if value == "" {
		return HistoryCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return HistoryCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return HistoryCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return HistoryCursor{}, fmt.Errorf("%w: invalid cursor time", ErrInvalidArgument)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return HistoryCursor{}, fmt.Errorf("%w: invalid cursor id", ErrInvalidArgument)
	}
	return HistoryCursor{CreatedAt: createdAt, ID: id}, nil
}

// BuildFareItems 用已锁定金额重建费用明细：试算快照不持久化拆项，
// 下单时按起步价 + 里程费 + 优惠券三段还原，总额以订单应付金额为准。
func BuildFareItems(rule PricingRule, originalCents, discountCents int64) []FareItem {
	base := rule.BaseFareCents
	if base <= 0 || base > originalCents {
		base = originalCents
	}
	items := []FareItem{{Type: "BASE_FARE", Name: "起步价", AmountCents: base}}
	if distance := originalCents - base; distance > 0 {
		items = append(items, FareItem{Type: "DISTANCE_FARE", Name: "里程费", AmountCents: distance})
	}
	if discountCents > 0 {
		items = append(items, FareItem{Type: "COUPON", Name: "优惠券", AmountCents: -discountCents})
	}
	return items
}

// BuildTripProgress 依据最新位置重算行程进度；无位置时回退到订单上的实际里程。
func BuildTripProgress(order *Order, location *LocationPoint) TripProgress {
	progress := TripProgress{}
	if order == nil {
		return progress
	}
	if location != nil && location.Longitude != 0 && location.Latitude != 0 {
		progress.SpeedKMH = location.SpeedKMH
		// 车端上报轨迹已带剩余距离/进度时直接采用，避免二次估算误差。
		if location.RemainingDistanceMeters > 0 || location.ProgressPercent > 0 {
			progress.RemainingDistanceMeters = location.RemainingDistanceMeters
			progress.RemainingDurationSeconds = location.RemainingDurationSeconds
			progress.ProgressPercent = min(100, max(0, location.ProgressPercent))
			return progress
		}
		remaining, duration := estimateRoute(
			Place{Longitude: location.Longitude, Latitude: location.Latitude},
			order.Destination,
		)
		progress.RemainingDistanceMeters = remaining
		progress.RemainingDurationSeconds = duration
		if order.DistanceMeters > 0 {
			percent := int32(100 - int64(remaining)*100/int64(order.DistanceMeters))
			progress.ProgressPercent = min(100, max(0, percent))
		}
		return progress
	}
	if order.Status == OrderStatusCompleted && order.ActualDistanceMeters > 0 {
		progress.ProgressPercent = 100
		return progress
	}
	return progress
}

func StatusPresentation(status OrderStatus) (string, string, int32, []string) {
	switch status {
	case OrderStatusPendingDispatch:
		return "PENDING_DISPATCH", "正在为你派车", 1, []string{"CANCEL"}
	case OrderStatusDispatching:
		return "DISPATCHING", "正在为你派车", 1, []string{"CANCEL"}
	case OrderStatusPickingUp:
		return "PICKING_UP", "车辆正在接驾", 2, []string{"CANCEL", "SHARE"}
	case OrderStatusWaitingBoarding:
		return "WAITING_BOARDING", "车辆已到达，请上车", 3, []string{"CANCEL", "UNLOCK_AND_START", "SHARE"}
	case OrderStatusInTrip:
		return "IN_TRIP", "行程进行中", 4, []string{"REQUEST_PULL_OVER", "SHARE"}
	case OrderStatusCompleted:
		return "COMPLETED", "已完成", 5, []string{"PAY", "REVIEW"}
	case OrderStatusCancelled:
		return "CANCELLED", "已取消", 5, nil
	default:
		return "EXCEPTION_CLOSED", "订单异常关闭", 5, nil
	}
}
