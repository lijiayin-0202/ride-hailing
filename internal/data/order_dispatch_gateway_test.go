package data

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOrderDispatchGatewayMapsOrderSnapshot(t *testing.T) {
	query := &fakeOrderDispatchQueryClient{reply: &v1.GetDispatchableOrderReply{
		OrderId: 81, ServiceAreaId: 9, OrderStatus: "WAITING",
		Pickup:        &v1.GeoPoint{StationId: 11, Longitude: 116.397, Latitude: 39.908, Address: "上车点"},
		Dropoff:       &v1.GeoPoint{StationId: 12, Longitude: 116.407, Latitude: 39.918, Address: "下车点"},
		IsSchedulable: true, ReservedAtUnixMilli: 1234,
	}}
	gateway := NewOrderDispatchGateway(query, &fakeOrderDispatchSyncClient{})

	snapshot, err := gateway.GetDispatchableOrder(context.Background(), 81)
	if err != nil {
		t.Fatalf("GetDispatchableOrder() error = %v", err)
	}
	if snapshot.OrderID != 81 || snapshot.ServiceAreaID != 9 || snapshot.Pickup.StationID != 11 || snapshot.Dropoff.StationID != 12 || !snapshot.Schedulable || !snapshot.ReservedAt.Equal(time.UnixMilli(1234).UTC()) {
		t.Fatalf("GetDispatchableOrder() = %#v, want mapped order snapshot", snapshot)
	}
}

func TestOrderDispatchGatewayMapsNotFound(t *testing.T) {
	query := &fakeOrderDispatchQueryClient{err: status.Error(codes.NotFound, "order absent")}
	gateway := NewOrderDispatchGateway(query, &fakeOrderDispatchSyncClient{})

	_, err := gateway.GetDispatchableOrder(context.Background(), 9)
	if !errors.Is(err, biz.ErrOrderNotFound) {
		t.Fatalf("GetDispatchableOrder() error = %v, want ErrOrderNotFound", err)
	}
}

func TestOrderDispatchGatewayMapsUnavailable(t *testing.T) {
	query := &fakeOrderDispatchQueryClient{err: status.Error(codes.Unavailable, "module two offline")}
	gateway := NewOrderDispatchGateway(query, &fakeOrderDispatchSyncClient{})

	_, err := gateway.GetDispatchableOrder(context.Background(), 9)
	if !errors.Is(err, biz.ErrOrderGatewayUnavailable) {
		t.Fatalf("GetDispatchableOrder() error = %v, want ErrOrderGatewayUnavailable", err)
	}
}

func TestOrderDispatchGatewayUsesDispatchNumberAsSyncKey(t *testing.T) {
	syncClient := &fakeOrderDispatchSyncClient{reply: &v1.SyncOrderDispatchStateReply{Accepted: true}}
	gateway := NewOrderDispatchGateway(&fakeOrderDispatchQueryClient{}, syncClient)
	event := biz.OrderDispatchEvent{DispatchNo: "DSP-1", OrderID: 2, DispatchID: 3, VehicleID: 4, Type: biz.OrderDispatchEventAttemptPending, OccurredAt: time.UnixMilli(88).UTC()}

	accepted, err := gateway.Sync(context.Background(), event)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !accepted || syncClient.request.GetIdempotencyKey() != "DSP-1" || syncClient.request.GetEventType() != v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_ATTEMPT_PENDING {
		t.Fatalf("Sync() request = %#v accepted = %t, want dispatch-number idempotency", syncClient.request, accepted)
	}
}

type fakeOrderDispatchQueryClient struct {
	request *v1.GetDispatchableOrderRequest
	reply   *v1.GetDispatchableOrderReply
	err     error
}

func (client *fakeOrderDispatchQueryClient) GetDispatchableOrder(_ context.Context, request *v1.GetDispatchableOrderRequest, _ ...grpc.CallOption) (*v1.GetDispatchableOrderReply, error) {
	client.request = request
	return client.reply, client.err
}

type fakeOrderDispatchSyncClient struct {
	request *v1.SyncOrderDispatchStateRequest
	reply   *v1.SyncOrderDispatchStateReply
	err     error
}

func (client *fakeOrderDispatchSyncClient) SyncDispatchState(_ context.Context, request *v1.SyncOrderDispatchStateRequest, _ ...grpc.CallOption) (*v1.SyncOrderDispatchStateReply, error) {
	client.request = request
	return client.reply, client.err
}
