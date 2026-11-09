package service

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestStructJSONRoundTrip(t *testing.T) {
	value, err := structpb.NewStruct(map[string]any{"speed": 12.5})
	if err != nil {
		t.Fatalf("NewStruct() error = %v", err)
	}
	decoded := unmarshalStruct(marshalStruct(value))
	if decoded.GetFields()["speed"].GetNumberValue() != 12.5 {
		t.Fatalf("round trip speed = %v", decoded.GetFields()["speed"])
	}
}

func TestSafetyIncidentsToPB(t *testing.T) {
	createdAt := time.Now().UTC()
	items := safetyIncidentsToPB([]*biz.SafetyIncident{{
		IncidentNo: "test-incident", IncidentType: 2, Severity: 3,
		Description: "test fault", Status: 0, CreatedAt: createdAt,
	}})
	if len(items) != 1 || items[0].GetIncidentNo() != "test-incident" || items[0].GetSeverity() != 3 {
		t.Fatalf("safetyIncidentsToPB() = %#v", items)
	}
}

func TestTimestampToTime(t *testing.T) {
	want := time.Now().UTC().Truncate(time.Second)
	got := timestampToTime(timestamppb.New(want))
	if got == nil || !got.Equal(want) {
		t.Fatalf("timestampToTime() = %v, want %v", got, want)
	}
	if timestampToTime(nil) != nil {
		t.Fatalf("timestampToTime(nil) should return nil")
	}
}

func TestUnmarshalStructInvalidJSON(t *testing.T) {
	value := unmarshalStruct([]byte(`not-json`))
	if len(value.GetFields()) != 0 {
		t.Fatalf("invalid JSON should produce an empty struct")
	}
}
