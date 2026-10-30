package service

import (
	"context"
	"errors"
	"testing"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestDispatchCallbackServiceMapsVehicleRejection(t *testing.T) {
	application := &fakeDispatchCallbackApplication{}
	service := NewDispatchCallbackService(application)
	request := &v1.ReportVehicleDispatchResponseRequest{
		IdempotencyKey: "DSP-1", DispatchId: 1, OrderId: 2, VehicleId: 3,
		ResponseType:    v1.VehicleDispatchResponseType_VEHICLE_DISPATCH_RESPONSE_TYPE_REJECTED,
		RejectionReason: "vehicle unavailable", RespondedAtUnixMilli: 1,
	}

	reply, err := service.ReportVehicleDispatchResponse(context.Background(), request)
	if err != nil {
		t.Fatalf("ReportVehicleDispatchResponse() error = %v", err)
	}
	if !reply.GetAccepted() || application.resolution.Status != biz.DispatchStatusRejected || application.resolution.DispatchID != 1 || application.resolution.OrderID != 2 || application.resolution.VehicleID != 3 || application.resolution.RespondedAtUnixMilli != 1 {
		t.Errorf("reply:%#v resolution:%#v, want accepted rejected result", reply, application.resolution)
	}
}

func TestDispatchCallbackServiceRejectsIncompleteResponse(t *testing.T) {
	service := NewDispatchCallbackService(&fakeDispatchCallbackApplication{})
	_, err := service.ReportVehicleDispatchResponse(context.Background(), &v1.ReportVehicleDispatchResponseRequest{
		DispatchId: 1, OrderId: 2, VehicleId: 3,
		ResponseType: v1.VehicleDispatchResponseType_VEHICLE_DISPATCH_RESPONSE_TYPE_REJECTED,
	})
	if !errors.Is(err, errInvalidDispatchCallback) {
		t.Fatalf("ReportVehicleDispatchResponse() error = %v, want errInvalidDispatchCallback", err)
	}
}

type fakeDispatchCallbackApplication struct {
	resolution biz.DispatchResolution
}

func (application *fakeDispatchCallbackApplication) Resolve(_ context.Context, resolution biz.DispatchResolution) (biz.DispatchRecord, error) {
	application.resolution = resolution
	return biz.DispatchRecord{ID: resolution.DispatchID, Status: resolution.Status}, nil
}
