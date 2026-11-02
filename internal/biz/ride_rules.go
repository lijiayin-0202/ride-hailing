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

func calculateFare(rule PricingRule, distance int32, coupon *Coupon) Fare {
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

func commandTarget(current OrderStatus, commandType string) (OrderStatus, error) {
	switch commandType {
	case "CANCEL":
		if current > OrderStatusArrived {
			return current, fmt.Errorf("%w: order cannot be cancelled", ErrConflict)
		}
		return OrderStatusCancelled, nil
	case "UNLOCK_AND_START":
		if current != OrderStatusArrived {
			return current, fmt.Errorf("%w: vehicle has not arrived", ErrConflict)
		}
		return OrderStatusInTrip, nil
	case "REQUEST_PULL_OVER":
		if current != OrderStatusInTrip {
			return current, fmt.Errorf("%w: trip is not in progress", ErrConflict)
		}
		return current, nil
	default:
		return current, fmt.Errorf("%w: unsupported command", ErrInvalidArgument)
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

func StatusPresentation(status OrderStatus) (string, string, int32, []string) {
	switch status {
	case OrderStatusMatching:
		return "MATCHING", "正在匹配车辆", 1, []string{"CANCEL"}
	case OrderStatusPickingUp:
		return "PICKING_UP", "车辆正在接驾", 1, []string{"CANCEL", "SHARE"}
	case OrderStatusArrived:
		return "VEHICLE_ARRIVED", "车辆已到达", 2, []string{"CANCEL", "UNLOCK_AND_START", "SHARE"}
	case OrderStatusInTrip:
		return "IN_TRIP", "行程进行中", 3, []string{"REQUEST_PULL_OVER", "SHARE"}
	case OrderStatusCompleted:
		return "COMPLETED", "已完成", 4, []string{"PAY", "REVIEW"}
	case OrderStatusCancelled:
		return "CANCELLED", "已取消", 4, nil
	default:
		return "CLOSED", "已关闭", 4, nil
	}
}
