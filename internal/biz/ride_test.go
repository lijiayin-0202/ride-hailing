package biz

import (
	"errors"
	"testing"
	"time"
)

func TestCalculateFareUsesIntegerCents(t *testing.T) {
	rule := PricingRule{BaseFareCents: 1260, BaseDistanceMeters: 3000, DistanceUnitPriceCents: 160}
	fare := calculateFare(rule, 42180, &QuoteCoupon{DiscountCents: 500})
	if fare.OriginalCents != 7660 {
		t.Fatalf("expected original 7660, actual %d", fare.OriginalCents)
	}
	if fare.DiscountCents != 500 || fare.PayableCents != 7160 {
		t.Fatalf("unexpected discount/payable: %d/%d", fare.DiscountCents, fare.PayableCents)
	}
}

func TestCalculateFareIncludesCouponBreakdown(t *testing.T) {
	rule := PricingRule{BaseFareCents: 1000, BaseDistanceMeters: 3000, DistanceUnitPriceCents: 100}
	fare := calculateFare(rule, 4000, &QuoteCoupon{DiscountCents: 200})
	if len(fare.Items) != 3 {
		t.Fatalf("expected 3 fare items, actual %d", len(fare.Items))
	}
	if fare.Items[2].Type != "COUPON" || fare.Items[2].AmountCents != -200 {
		t.Fatalf("unexpected coupon item: %+v", fare.Items[2])
	}
}

func TestHistoryCursorRoundTrip(t *testing.T) {
	want := HistoryCursor{CreatedAt: time.Date(2026, 10, 28, 12, 30, 0, 123, time.UTC), ID: 42}
	actual, err := DecodeHistoryCursor(EncodeHistoryCursor(want))
	if err != nil {
		t.Fatal(err)
	}
	if !actual.CreatedAt.Equal(want.CreatedAt) || actual.ID != want.ID {
		t.Fatalf("expected %+v, actual %+v", want, actual)
	}
}

func TestDecodeHistoryCursorRejectsInvalidValue(t *testing.T) {
	_, err := DecodeHistoryCursor("not-base64")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, actual %v", err)
	}
}

func TestValidateScheduledQuote(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	quote := &Quote{
		CityCode: "420100", RideMode: "SCHEDULED", ScheduledAt: &now,
		Origin:      Place{PlaceID: "origin", Longitude: 114.1, Latitude: 30.1},
		Destination: Place{PlaceID: "destination", Longitude: 114.2, Latitude: 30.2},
	}
	if err := validateQuote(quote, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid scheduled time, actual %v", err)
	}
}

func TestValidReviewTags(t *testing.T) {
	if !validReviewTags([]string{"SMOOTH", "CLEAN"}) {
		t.Fatal("expected known tags to be valid")
	}
	if validReviewTags([]string{"UNKNOWN"}) {
		t.Fatal("expected unknown tag to be rejected")
	}
}

func TestValidPhone(t *testing.T) {
	if !validPhone("13812345678") {
		t.Fatal("expected mobile phone to be valid")
	}
	if validPhone("1234") || validPhone("1381234abcd") {
		t.Fatal("expected invalid phone to be rejected")
	}
}

func TestBuildFareItemsMatchesTotals(t *testing.T) {
	rule := PricingRule{BaseFareCents: 1000}
	items := BuildFareItems(rule, 2000, 300)
	total := int64(0)
	for _, item := range items {
		total += item.AmountCents
	}
	// 明细合计应等于应付金额（原始金额 - 优惠）。
	if total != 1700 {
		t.Fatalf("expected items to equal payable amount, actual %d", total)
	}
	if items[0].Type != "BASE_FARE" || items[1].Type != "DISTANCE_FARE" || items[2].AmountCents != -300 {
		t.Fatalf("unexpected items: %+v", items)
	}
	if base := BuildFareItems(PricingRule{BaseFareCents: 5000}, 2000, 0); base[0].AmountCents != 2000 {
		t.Fatalf("base fare must be capped by original amount, actual %d", base[0].AmountCents)
	}
	if noDiscount := BuildFareItems(rule, 1000, 0); len(noDiscount) != 1 {
		t.Fatalf("expected single item without distance or discount, actual %+v", noDiscount)
	}
}
