package data

import (
	"errors"
	"testing"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestIsOptionalRelationError(t *testing.T) {
	if !isOptionalRelationError(errors.New(`ERROR: relation "dispatch_records" does not exist (SQLSTATE 42P01)`)) {
		t.Fatal("expected undefined table to be optional")
	}
	if !isOptionalRelationError(errors.New(`ERROR: column "reported_at" does not exist (SQLSTATE 42703)`)) {
		t.Fatal("expected undefined column to be optional")
	}
	if isOptionalRelationError(errors.New("connection refused")) {
		t.Fatal("connection errors must not be ignored")
	}
	if isOptionalRelationError(nil) {
		t.Fatal("nil error must not be treated as optional")
	}
}

func TestMapHelpersReadCrossModuleColumns(t *testing.T) {
	occurredAt := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	row := map[string]any{
		"dispatch_strategy":           "NEAREST",
		"strategy_version":            []byte("v2"),
		"attempt_no":                  int64(3),
		"estimated_pickup_distance_m": "1200.5",
		"fee_amount":                  "5.00",
		"reported_at":                 occurredAt,
		"tags":                        []byte(`["CLEAN"]`),
	}
	if got := mapString(row, "dispatch_strategy", "strategy_name"); got != "NEAREST" {
		t.Fatalf("unexpected strategy: %s", got)
	}
	if got := mapString(row, "strategy_name", "strategy_version"); got != "v2" {
		t.Fatalf("unexpected strategy version: %s", got)
	}
	if got := mapInt64(row, "attempt_count", "attempt_no"); got != 3 {
		t.Fatalf("unexpected attempt count: %d", got)
	}
	if got := mapFloat(row, "pickup_distance_meters", "estimated_pickup_distance_m"); got != 1200.5 {
		t.Fatalf("unexpected distance: %f", got)
	}
	if got := mapCents(row, "final_amount", "fee_amount"); got != 500 {
		t.Fatalf("unexpected cents: %d", got)
	}
	if got := mapTime(row, "reported_at"); got == nil || !got.Equal(occurredAt) {
		t.Fatalf("unexpected time: %v", got)
	}
	if got := decodeTags(mapBytes(row, "tags")); len(got) != 1 || got[0] != "CLEAN" {
		t.Fatalf("unexpected tags: %v", got)
	}
	if got := mapString(row, "missing"); got != "" {
		t.Fatalf("missing column must be empty, actual %q", got)
	}
}

func TestChangeSourceMapping(t *testing.T) {
	tests := map[string]int16{
		biz.CommandUnlockAndStart:  1,
		biz.CommandRequestPullOver: 1,
		biz.CommandMarkArrived:     2,
		biz.CommandCompleteTrip:    2,
		biz.CommandDispatch:        3,
		biz.CommandDispatchAccept:  3,
		biz.CommandCloseException:  4,
		"UNKNOWN":                  5,
	}
	for command, want := range tests {
		if got := changeSource(command); got != want {
			t.Fatalf("command %s: expected %d, actual %d", command, want, got)
		}
	}
}
