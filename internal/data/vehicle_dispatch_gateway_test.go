package data

import (
	"context"
	"testing"
	"time"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"google.golang.org/grpc"
)

func TestGRPCVehicleDispatchGatewayMapsInstruction(t *testing.T) {
	client := &fakeVehicleDispatchClient{reply: &v1.SendDispatchInstructionReply{AcceptedForDelivery: true}}
	gateway := NewGRPCVehicleDispatchGateway(client)
	input := biz.VehicleDispatchInstruction{
		Dispatch:             biz.DispatchRecord{ID: 1, DispatchNo: "DSP-1", OrderID: 2, VehicleID: 3, AttemptNo: 1},
		Pickup:               biz.DispatchLocation{StationID: 4, Longitude: 116.397128, Latitude: 39.916527, Address: "起点"},
		Dropoff:              biz.DispatchLocation{StationID: 5, Longitude: 116.407128, Latitude: 39.926527, Address: "终点"},
		ConfirmationDeadline: time.Date(2026, time.October, 28, 10, 0, 30, 0, time.UTC),
	}

	accepted, err := gateway.Send(context.Background(), input)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !accepted || client.request.GetInstruction().GetIdempotencyKey() != "DSP-1" || client.request.GetInstruction().GetPickup().GetStationId() != 4 || client.request.GetInstruction().GetConfirmationDeadlineUnixMilli() != input.ConfirmationDeadline.UnixMilli() {
		t.Errorf("Send() request = %#v, accepted = %t", client.request, accepted)
	}
}

type fakeVehicleDispatchClient struct {
	request *v1.SendDispatchInstructionRequest
	reply   *v1.SendDispatchInstructionReply
	err     error
}

func (client *fakeVehicleDispatchClient) SendDispatchInstruction(_ context.Context, request *v1.SendDispatchInstructionRequest, _ ...grpc.CallOption) (*v1.SendDispatchInstructionReply, error) {
	client.request = request
	return client.reply, client.err
}
