package data

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

func TestLocalModule2Module4DispatchLifecycle(t *testing.T) {
	harness := newLocalDispatchHarness(t)
	first, err := harness.application.Assign(context.Background(), harness.orderID)
	if err != nil {
		t.Fatalf("first Assign() error = %v", err)
	}
	harness.expectInstruction(t, first.Record.ID)
	harness.expectEvent(t, first.Record.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_ATTEMPT_PENDING)

	harness.reject(t, first.Record)
	harness.expectEvent(t, first.Record.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_REJECTED)
	second := harness.reassign(t, first.Record.ID)
	harness.expectInstruction(t, second.ID)
	harness.expectEvent(t, second.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_ATTEMPT_PENDING)

	harness.timeout(t, second)
	harness.expectEvent(t, second.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_TIMED_OUT)
	third := harness.reassign(t, second.ID)
	harness.expectInstruction(t, third.ID)
	harness.expectEvent(t, third.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_ATTEMPT_PENDING)

	harness.timeout(t, third)
	harness.expectEvent(t, third.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_TIMED_OUT)
	harness.reassign(t, third.ID)
	harness.expectEvent(t, third.ID, v1.OrderDispatchEventType_ORDER_DISPATCH_EVENT_TYPE_EXHAUSTED)
}

type localDispatchHarness struct {
	repository  *DispatchRepository
	application *biz.AutoDispatchUsecase
	module2     *localModule2
	module4     *localModule4
	orderID     int64
}

func newLocalDispatchHarness(t *testing.T) *localDispatchHarness {
	t.Helper()
	pool := openModule3TestPool(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	areaID, otherAreaID := base+1, base+2
	vehicleOne, vehicleTwo := base+3, base+4
	seedDispatchCandidateData(t, ctx, pool, areaID, otherAreaID, vehicleOne, vehicleTwo)
	vehicleThree := base + 30
	seedVehicleWithStatus(t, ctx, pool, vehicleThree, areaID, 1, 1, true, nil, time.Now().UTC())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicle_status_logs WHERE vehicle_id=$1", vehicleThree)
		_, _ = pool.Exec(context.Background(), "DELETE FROM public.vehicles WHERE id=$1", vehicleThree)
	})
	setLatestVehiclePosition(t, ctx, pool, vehicleOne, 116.397128, 39.916527)
	setLatestVehiclePosition(t, ctx, pool, vehicleTwo, 116.407128, 39.916527)
	setLatestVehiclePosition(t, ctx, pool, vehicleThree, 116.417128, 39.916527)
	orderID := base + 20
	seedTripOrder(t, ctx, pool, orderID)
	generator, err := NewSnowflakeDispatchIDGenerator(1)
	if err != nil {
		t.Fatalf("new test ID generator: %v", err)
	}
	repository := NewDispatchRepository(&Data{pool: pool, dispatchIDGenerator: generator})
	t.Cleanup(func() { cleanupIntegrationOrder(t, pool, orderID) })

	module2 := startLocalModule2(t, orderID, areaID)
	module4 := startLocalModule4(t)
	config := &conf.Data{DispatchOutbox: &conf.Data_DispatchOutbox{
		OrderGatewayAddr: module2.address, VehicleGatewayAddr: module4.address,
		RequestTimeout: durationpb.New(time.Second), PollInterval: durationpb.New(10 * time.Millisecond),
		OrderSyncPollInterval: durationpb.New(10 * time.Millisecond), LeaseDuration: durationpb.New(time.Second),
	}}
	query := NewConfiguredOrderQueryGateway(config)
	application := biz.NewAutoDispatchUsecase(query, repository, biz.NewSystemClock())
	vehicleDispatcher := NewDispatchOutboxDispatcher(config, repository, log.NewStdLogger(testDiscard{}))
	orderDispatcher := NewOrderDispatchOutboxDispatcher(config, repository, log.NewStdLogger(testDiscard{}))
	if err := vehicleDispatcher.Start(ctx); err != nil {
		t.Fatalf("start module 4 dispatcher: %v", err)
	}
	if err := orderDispatcher.Start(ctx); err != nil {
		t.Fatalf("start module 2 dispatcher: %v", err)
	}
	t.Cleanup(func() { stopLocalDispatchers(t, vehicleDispatcher, orderDispatcher) })
	return &localDispatchHarness{repository: repository, application: application, module2: module2, module4: module4, orderID: orderID}
}

func (harness *localDispatchHarness) reject(t *testing.T, record biz.DispatchRecord) {
	t.Helper()
	_, err := harness.repository.ResolveAndQueue(context.Background(), biz.DispatchResolution{DispatchID: record.ID, OrderID: record.OrderID,
		VehicleID: record.VehicleID, Status: biz.DispatchStatusRejected, FailureReason: "local vehicle rejected"}, time.Now().UTC())
	if err != nil {
		t.Fatalf("reject dispatch: %v", err)
	}
}

func (harness *localDispatchHarness) timeout(t *testing.T, record biz.DispatchRecord) {
	t.Helper()
	_, err := harness.repository.ResolveAndQueue(context.Background(), biz.DispatchResolution{DispatchID: record.ID, OrderID: record.OrderID,
		VehicleID: record.VehicleID, Status: biz.DispatchStatusTimedOut, FailureReason: "confirmation timeout"}, time.Now().UTC())
	if err != nil {
		t.Fatalf("timeout dispatch: %v", err)
	}
}

func (harness *localDispatchHarness) reassign(t *testing.T, triggerID int64) biz.DispatchRecord {
	t.Helper()
	if _, err := harness.repository.data.pool.Exec(context.Background(), "UPDATE public.dispatch_reassignment_jobs SET next_attempt_at=$2 WHERE trigger_dispatch_id=$1", triggerID, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("prioritize reassignment: %v", err)
	}
	processed, err := harness.repository.ProcessReassignmentOnce(context.Background(), harness.application, time.Now().UTC(), time.Second)
	if err != nil || !processed {
		t.Fatalf("process reassignment=%t error=%v", processed, err)
	}
	row := harness.repository.data.pool.QueryRow(context.Background(), `SELECT id,dispatch_no,order_id,vehicle_id,attempt_no,dispatch_strategy,status,failure_reason,dispatched_at,responded_at
		FROM public.dispatch_records WHERE order_id=$1 ORDER BY attempt_no DESC LIMIT 1`, harness.orderID)
	record, err := scanDispatchRecord(row)
	if err != nil {
		t.Fatalf("read reassigned dispatch: %v", err)
	}
	return record
}

func (harness *localDispatchHarness) expectInstruction(t *testing.T, dispatchID int64) {
	waitForExpectedInstruction(t, harness.module4.instructions, dispatchID)
}
func (harness *localDispatchHarness) expectEvent(t *testing.T, dispatchID int64, eventType v1.OrderDispatchEventType) {
	harness.module2.waitEvent(t, dispatchID, eventType)
}

type testDiscard struct{}

func (testDiscard) Write([]byte) (int, error) { return 0, nil }

type localModule2 struct {
	v1.UnimplementedOrderDispatchQueryServiceServer
	v1.UnimplementedOrderDispatchSyncServiceServer
	address         string
	orderID, areaID int64
	events          chan *v1.SyncOrderDispatchStateRequest
}

func startLocalModule2(t *testing.T, orderID, areaID int64) *localModule2 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	service := &localModule2{address: listener.Addr().String(), orderID: orderID, areaID: areaID, events: make(chan *v1.SyncOrderDispatchStateRequest, 16)}
	server := grpc.NewServer()
	v1.RegisterOrderDispatchQueryServiceServer(server, service)
	v1.RegisterOrderDispatchSyncServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return service
}
func (service *localModule2) GetDispatchableOrder(context.Context, *v1.GetDispatchableOrderRequest) (*v1.GetDispatchableOrderReply, error) {
	return &v1.GetDispatchableOrderReply{OrderId: service.orderID, ServiceAreaId: service.areaID, IsSchedulable: true, Pickup: &v1.GeoPoint{Longitude: 116.397128, Latitude: 39.916527}, Dropoff: &v1.GeoPoint{Longitude: 116.417128, Latitude: 39.926527}}, nil
}
func (service *localModule2) SyncDispatchState(_ context.Context, request *v1.SyncOrderDispatchStateRequest) (*v1.SyncOrderDispatchStateReply, error) {
	service.events <- request
	return &v1.SyncOrderDispatchStateReply{Accepted: true}, nil
}
func (service *localModule2) waitEvent(t *testing.T, dispatchID int64, eventType v1.OrderDispatchEventType) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-service.events:
			if event.GetDispatchId() == dispatchID && event.GetEventType() == eventType {
				return
			}
		case <-deadline:
			t.Fatalf("missing module 2 event dispatch=%d type=%s", dispatchID, eventType)
		}
	}
}

type localModule4 struct {
	v1.UnimplementedVehicleDispatchGatewayServiceServer
	address      string
	instructions chan *v1.SendDispatchInstructionRequest
}

func startLocalModule4(t *testing.T) *localModule4 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	service := &localModule4{address: listener.Addr().String(), instructions: make(chan *v1.SendDispatchInstructionRequest, 16)}
	server := grpc.NewServer()
	v1.RegisterVehicleDispatchGatewayServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return service
}
func (service *localModule4) SendDispatchInstruction(_ context.Context, request *v1.SendDispatchInstructionRequest) (*v1.SendDispatchInstructionReply, error) {
	service.instructions <- request
	return &v1.SendDispatchInstructionReply{AcceptedForDelivery: true}, nil
}
func waitForExpectedInstruction(t *testing.T, instructions <-chan *v1.SendDispatchInstructionRequest, dispatchID int64) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case request := <-instructions:
			if request.GetInstruction().GetDispatchId() == dispatchID {
				return
			}
		case <-deadline:
			t.Fatalf("missing module 4 instruction dispatch=%d", dispatchID)
		}
	}
}

func cleanupIntegrationOrder(t *testing.T, pool *pgxpool.Pool, orderID int64) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM public.dispatch_reassignment_jobs WHERE order_id=$1", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM public.order_dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id=$1)", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM public.dispatch_outbox WHERE dispatch_id IN (SELECT id FROM public.dispatch_records WHERE order_id=$1)", orderID)
	_, _ = pool.Exec(ctx, "DELETE FROM public.dispatch_records WHERE order_id=$1", orderID)
}
func stopLocalDispatchers(t *testing.T, vehicle *DispatchOutboxDispatcher, order *OrderDispatchOutboxDispatcher) {
}
