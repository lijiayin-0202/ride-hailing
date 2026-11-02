package biz

import (
	"errors"
	"testing"
	"time"
)

func TestCommandTarget(t *testing.T) {
	tests := []struct {
		name        string
		current     OrderStatus
		commandType string
		want        OrderStatus
		wantErr     error
	}{
		{name: "cancel matching", current: OrderStatusMatching, commandType: "CANCEL", want: OrderStatusCancelled},
		{name: "start after arrival", current: OrderStatusArrived, commandType: "UNLOCK_AND_START", want: OrderStatusInTrip},
		{name: "reject early start", current: OrderStatusPickingUp, commandType: "UNLOCK_AND_START", want: OrderStatusPickingUp, wantErr: ErrConflict},
		{name: "pull over in trip", current: OrderStatusInTrip, commandType: "REQUEST_PULL_OVER", want: OrderStatusInTrip},
		{name: "reject unknown", current: OrderStatusMatching, commandType: "UNKNOWN", want: OrderStatusMatching, wantErr: ErrInvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := commandTarget(test.current, test.commandType)
			if actual != test.want {
				t.Fatalf("expected status %d, actual %d", test.want, actual)
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("expected error %v, actual %v", test.wantErr, err)
			}
		})
	}
}

func TestCalculateFareUsesIntegerCents(t *testing.T) {
	rule := PricingRule{BaseFareCents: 1260, BaseDistanceMeters: 3000, DistanceUnitPriceCents: 160}
	fare := calculateFare(rule, 42180, &Coupon{DiscountCents: 500})
	if fare.OriginalCents != 7660 {
		t.Fatalf("expected original 7660, actual %d", fare.OriginalCents)
	}
	if fare.DiscountCents != 500 || fare.PayableCents != 7160 {
		t.Fatalf("unexpected discount/payable: %d/%d", fare.DiscountCents, fare.PayableCents)
	}
}

func TestCalculateFareIncludesCouponBreakdown(t *testing.T) {
	rule := PricingRule{BaseFareCents: 1000, BaseDistanceMeters: 3000, DistanceUnitPriceCents: 100}
	fare := calculateFare(rule, 4000, &Coupon{DiscountCents: 200})
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
