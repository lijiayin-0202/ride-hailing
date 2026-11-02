package data

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type rideRepo struct {
	data *Data
}

func NewRideRepo(data *Data) biz.RideRepo { return &rideRepo{data: data} }

func (r *rideRepo) ListServiceTypes(ctx context.Context, cityCode string, now time.Time) ([]biz.ServiceType, error) {
	var rows []serviceTypePricingRow
	err := r.data.db.WithContext(ctx).Table("vehicle_service_types AS vst").
		Select(`vst.id, vst.service_type_code, vst.display_name, vst.tags,
            vst.seat_count, vst.pickup_eta_seconds, pr.id AS pricing_rule_id,
            pr.version, pr.base_fare, pr.base_distance_m,
            ROUND(pr.distance_unit_price * 100)::bigint AS distance_unit_price_cents`).
		Joins(`JOIN pricing_rules AS pr ON pr.service_type_id = vst.id
            AND pr.status = 1 AND pr.effective_from <= ?
            AND (pr.effective_to IS NULL OR pr.effective_to > ?)`, now, now).
		Where("vst.city_code = ? AND vst.status = 1", cityCode).
		Order("vst.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]biz.ServiceType, 0, len(rows))
	for _, row := range rows {
		result = append(result, serviceTypeFromRow(row))
	}
	return result, nil
}

func (r *rideRepo) FindCoupon(ctx context.Context, passengerID int64, couponID string, originalCents int64) (*biz.Coupon, error) {
	couponPK, err := parsePublicID(couponID)
	if err != nil {
		return nil, biz.ErrNotFound
	}
	var row struct {
		PassengerCouponID int64
		CouponID          int64
		Name              string
		CouponType        int16
		DiscountAmount    cents
		DiscountRate      float64
		MaxDiscountAmount cents
		ValidTo           time.Time
	}
	err = r.data.db.WithContext(ctx).Table("passenger_coupons AS pc").
		Select(`pc.id AS passenger_coupon_id, c.id AS coupon_id, c.coupon_name AS name,
			c.coupon_type, c.discount_amount, c.discount_rate,
			0 AS max_discount_amount,
			pc.expires_at AS valid_to`).
		Joins("JOIN coupons AS c ON c.id = pc.coupon_id").
		Where(`pc.id = ? AND pc.passenger_id = ? AND pc.status = 0
			AND pc.expires_at > CURRENT_TIMESTAMP AND c.status = 1
			AND c.minimum_order_amount <= ?`, couponPK, passengerID, cents(originalCents)).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, biz.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	discount := int64(row.DiscountAmount)
	if row.CouponType == 2 {
		discount = int64(math.Round(float64(originalCents) * (1 - row.DiscountRate)))
		if row.MaxDiscountAmount > 0 {
			discount = min(discount, int64(row.MaxDiscountAmount))
		}
	}
	return &biz.Coupon{
		ID: row.CouponID, Name: row.Name, DiscountCents: discount,
		ValidUntil: row.ValidTo, PassengerCoupon: row.PassengerCouponID,
	}, nil
}

func (r *rideRepo) CreateQuote(ctx context.Context, quote *biz.Quote) error {
	var serviceArea struct{ ID int64 }
	err := r.data.db.WithContext(ctx).Table("service_areas").
		Select("id").Where("city_code = ? AND status = 1", quote.CityCode).
		Order("id").Take(&serviceArea).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = r.data.db.WithContext(ctx).Table("stations AS origin").
			Select("origin.service_area_id AS id").
			Joins("JOIN stations destination ON destination.service_area_id = origin.service_area_id").
			Joins("JOIN service_areas area ON area.id = origin.service_area_id AND area.status = 1").
			Where(`(origin.station_code = ? OR origin.station_name = ?)
				AND (destination.station_code = ? OR destination.station_name = ?)`,
				quote.Origin.PlaceID, quote.Origin.PlaceID,
				quote.Destination.PlaceID, quote.Destination.PlaceID).
			Take(&serviceArea).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return biz.ErrNotFound
	}
	if err != nil {
		return err
	}
	quote.ServiceAreaID = serviceArea.ID
	row := quoteToRow(quote)
	return r.data.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		for _, option := range quote.Options {
			item := &quoteOptionRow{
				QuoteID: row.ID, ServiceTypeID: option.ServiceType.ID,
				PricingRuleID:    option.ServiceType.Rule.ID,
				PickupETASeconds: option.ServiceType.PickupETASeconds,
				OriginalAmount:   cents(option.Fare.OriginalCents),
				DiscountAmount:   cents(option.Fare.DiscountCents),
				PayableAmount:    cents(option.Fare.PayableCents), CreatedAt: time.Now(),
			}
			if err := tx.Create(item).Error; err != nil {
				return err
			}
		}
		quote.ID = row.ID
		return nil
	})
}

type quoteOptionDetail struct {
	ServiceTypeID int64
	Code          string
	DisplayName   string
	Tags          []byte
	SeatCount     int32
	ETA           int32
	Original      cents
	Discount      cents
	Payable       cents
	PricingRuleID int64
	Version       string
}

func (r *rideRepo) GetQuote(ctx context.Context, token string) (*biz.Quote, error) {
	var row quoteRow
	err := r.data.db.WithContext(ctx).
		Where("quote_token = ? AND expires_at > CURRENT_TIMESTAMP", token).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, biz.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var options []quoteOptionDetail
	err = r.data.db.WithContext(ctx).Table("fare_quote_options AS fqo").
		Select(`fqo.service_type_id, vst.service_type_code AS code,
            vst.display_name, vst.tags, vst.seat_count,
            fqo.pickup_eta_seconds AS eta, fqo.original_amount AS original,
            fqo.discount_amount AS discount, fqo.payable_amount AS payable,
            pr.id AS pricing_rule_id, pr.version`).
		Joins("JOIN vehicle_service_types AS vst ON vst.id = fqo.service_type_id").
		Joins("JOIN pricing_rules AS pr ON pr.id = fqo.pricing_rule_id").
		Where("fqo.quote_id = ?", row.ID).Scan(&options).Error
	if err != nil {
		return nil, err
	}
	return hydrateQuote(row, options), nil
}

func hydrateQuote(row quoteRow, options []quoteOptionDetail) *biz.Quote {
	quote := quoteFromRow(row)
	if row.PassengerCouponID != nil {
		quote.Coupon = &biz.Coupon{PassengerCoupon: *row.PassengerCouponID}
	}
	for _, option := range options {
		quote.Options = append(quote.Options, biz.QuoteOption{
			ServiceType: biz.ServiceType{
				ID: option.ServiceTypeID, Code: option.Code, DisplayName: option.DisplayName,
				Tags: decodeTags(option.Tags), SeatCount: option.SeatCount,
				PickupETASeconds: option.ETA,
				Rule:             biz.PricingRule{ID: option.PricingRuleID, Version: option.Version},
			},
			Fare: biz.Fare{OriginalCents: int64(option.Original),
				DiscountCents: int64(option.Discount), PayableCents: int64(option.Payable)},
		})
	}
	return quote
}

func serviceTypeFromRow(row serviceTypePricingRow) biz.ServiceType {
	return biz.ServiceType{
		ID: row.ID, Code: row.ServiceTypeCode, DisplayName: row.DisplayName,
		Tags: decodeTags(row.Tags), SeatCount: row.SeatCount,
		PickupETASeconds: row.PickupETASeconds,
		Rule: biz.PricingRule{
			ID: row.PricingRuleID, Version: row.Version,
			BaseFareCents: int64(row.BaseFare), BaseDistanceMeters: row.BaseDistanceM,
			DistanceUnitPriceCents: row.DistanceUnitPriceCents,
		},
	}
}

func quoteToRow(quote *biz.Quote) *quoteRow {
	row := &quoteRow{
		QuoteToken: quote.Token, ServiceAreaID: quote.ServiceAreaID,
		CityCode: quote.CityCode, RideMode: quote.RideMode, ScheduledAt: quote.ScheduledAt,
		OriginPlaceID: quote.Origin.PlaceID, OriginName: placeName(quote.Origin),
		OriginAddress: quote.Origin.Address, OriginLongitude: quote.Origin.Longitude,
		OriginLatitude: quote.Origin.Latitude, DestinationPlaceID: quote.Destination.PlaceID,
		DestinationName: placeName(quote.Destination), DestinationAddress: quote.Destination.Address,
		DestinationLongitude: quote.Destination.Longitude, DestinationLatitude: quote.Destination.Latitude,
		DistanceMeters: quote.DistanceMeters, EstimatedDurationSeconds: quote.EstimatedDurationSeconds,
		Polyline: quote.Polyline, ExpiresAt: quote.ExpiresAt, CreatedAt: time.Now(),
	}
	if quote.PassengerID > 0 {
		row.PassengerID = &quote.PassengerID
	}
	if quote.Coupon != nil {
		row.PassengerCouponID = &quote.Coupon.PassengerCoupon
	}
	return row
}

func quoteFromRow(row quoteRow) *biz.Quote {
	quote := &biz.Quote{
		ID: row.ID, Token: row.QuoteToken, ServiceAreaID: row.ServiceAreaID,
		CityCode: row.CityCode, RideMode: row.RideMode, ScheduledAt: row.ScheduledAt,
		Origin: biz.Place{PlaceID: row.OriginPlaceID, Name: row.OriginName,
			Address: row.OriginAddress, Longitude: row.OriginLongitude, Latitude: row.OriginLatitude},
		Destination: biz.Place{PlaceID: row.DestinationPlaceID, Name: row.DestinationName,
			Address: row.DestinationAddress, Longitude: row.DestinationLongitude,
			Latitude: row.DestinationLatitude},
		DistanceMeters:           row.DistanceMeters,
		EstimatedDurationSeconds: row.EstimatedDurationSeconds,
		Polyline:                 row.Polyline, ExpiresAt: row.ExpiresAt,
	}
	if row.PassengerID != nil {
		quote.PassengerID = *row.PassengerID
	}
	return quote
}

func placeName(place biz.Place) string {
	if place.Name != "" {
		return place.Name
	}
	return place.PlaceID
}
