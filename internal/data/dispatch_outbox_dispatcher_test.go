package data

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

func TestDispatchOutboxDispatcherDisablesWithoutGatewayAddress(t *testing.T) {
	dispatcher := NewDispatchOutboxDispatcher(&conf.Data{}, nil, log.NewStdLogger(io.Discard))
	if dispatcher.Enabled() {
		t.Fatal("dispatcher should be disabled without a module 4 gateway address")
	}
	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("disabled Start() error = %v", err)
	}
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("disabled Stop() error = %v", err)
	}
}

func TestDispatchOutboxDispatcherStopWaitsForWorker(t *testing.T) {
	processor := &blockingOutboxProcessor{started: make(chan struct{})}
	workerContext, cancel := context.WithCancel(context.Background())
	dispatcher := &DispatchOutboxDispatcher{
		repository: processor, requestTimeout: time.Second, pollInterval: time.Hour,
		leaseDuration: time.Second, log: log.NewHelper(log.NewStdLogger(io.Discard)),
		cancel: cancel, done: make(chan struct{}),
	}
	go dispatcher.run(workerContext, acceptingDispatchGateway{}, dispatcher.done)
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("dispatcher worker did not start")
	}
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestDispatchOutboxDispatcherDeliversToLocalModule4GRPCServer(t *testing.T) {
	repository, ctx, instruction := setupOutboxInstruction(t)
	if _, err := repository.Queue(ctx, instruction); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	address, receiver := startLocalVehicleGateway(t)
	dispatcher := NewDispatchOutboxDispatcher(&conf.Data{DispatchOutbox: &conf.Data_DispatchOutbox{
		VehicleGatewayAddr: address, RequestTimeout: durationpb.New(time.Second),
		PollInterval: durationpb.New(10 * time.Millisecond), LeaseDuration: durationpb.New(time.Second),
	}}, repository, log.NewStdLogger(io.Discard))
	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := dispatcher.Stop(stopContext); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	request := waitForExpectedDispatchInstruction(t, receiver.requests, instruction.Dispatch.ID)
	if request.GetInstruction().GetIdempotencyKey() != instruction.Dispatch.DispatchNo {
		t.Errorf("gRPC instruction = %#v, want key %q", request.GetInstruction(), instruction.Dispatch.DispatchNo)
	}
	waitForOutboxDelivery(t, ctx, repository, instruction.Dispatch.ID)
}

func startLocalVehicleGateway(t *testing.T) (string, *localVehicleGateway) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen local module 4 gateway: %v", err)
	}
	server := grpc.NewServer()
	receiver := &localVehicleGateway{requests: make(chan *v1.SendDispatchInstructionRequest, 1)}
	v1.RegisterVehicleDispatchGatewayServiceServer(server, receiver)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String(), receiver
}

func waitForDispatchInstruction(t *testing.T, requests <-chan *v1.SendDispatchInstructionRequest) *v1.SendDispatchInstructionRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("module 4 gateway did not receive dispatch instruction")
		return nil
	}
}

func waitForExpectedDispatchInstruction(t *testing.T, requests <-chan *v1.SendDispatchInstructionRequest, dispatchID int64) *v1.SendDispatchInstructionRequest {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case request := <-requests:
			if request.GetInstruction().GetDispatchId() == dispatchID {
				return request
			}
		case <-deadline:
			t.Fatalf("module 4 gateway did not receive dispatch %d", dispatchID)
		}
	}
}

func waitForOutboxDelivery(t *testing.T, ctx context.Context, repository *DispatchRepository, dispatchID int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var status int16
		err := repository.data.pool.QueryRow(ctx, "SELECT status FROM public.dispatch_outbox WHERE dispatch_id = $1", dispatchID).Scan(&status)
		if err == nil && status == dispatchOutboxDelivered {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("outbox was not marked delivered after module 4 accepted instruction")
}

type blockingOutboxProcessor struct {
	started chan struct{}
}

type localVehicleGateway struct {
	v1.UnimplementedVehicleDispatchGatewayServiceServer
	requests chan *v1.SendDispatchInstructionRequest
}

func (gateway *localVehicleGateway) SendDispatchInstruction(_ context.Context, request *v1.SendDispatchInstructionRequest) (*v1.SendDispatchInstructionReply, error) {
	gateway.requests <- request
	return &v1.SendDispatchInstructionReply{AcceptedForDelivery: true}, nil
}

func (processor *blockingOutboxProcessor) ProcessOutboxOnce(ctx context.Context, _ biz.VehicleDispatchGateway, _ time.Time, _ time.Duration) (bool, error) {
	select {
	case <-processor.started:
	default:
		close(processor.started)
	}
	<-ctx.Done()
	return false, nil
}
