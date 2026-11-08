package biz

import (
	"errors"
	"testing"
)

func TestTransitionMatrix(t *testing.T) {
	legal := [][2]OrderStatus{
		{OrderStatusPendingDispatch, OrderStatusDispatching},
		{OrderStatusPendingDispatch, OrderStatusCancelled},
		{OrderStatusDispatching, OrderStatusPickingUp},
		{OrderStatusDispatching, OrderStatusPendingDispatch},
		{OrderStatusPickingUp, OrderStatusWaitingBoarding},
		{OrderStatusPickingUp, OrderStatusCancelled},
		{OrderStatusWaitingBoarding, OrderStatusInTrip},
		{OrderStatusWaitingBoarding, OrderStatusCancelled},
		{OrderStatusInTrip, OrderStatusCompleted},
		{OrderStatusInTrip, OrderStatusExceptionClosed},
		{OrderStatusPendingDispatch, OrderStatusExceptionClosed},
	}
	for _, pair := range legal {
		if !CanTransit(pair[0], pair[1]) {
			t.Fatalf("expected transition %d -> %d to be legal", pair[0], pair[1])
		}
	}

	illegal := [][2]OrderStatus{
		{OrderStatusPendingDispatch, OrderStatusInTrip},
		{OrderStatusPendingDispatch, OrderStatusCompleted},
		{OrderStatusPickingUp, OrderStatusInTrip},
		{OrderStatusInTrip, OrderStatusCancelled},
		{OrderStatusCompleted, OrderStatusCancelled},
		{OrderStatusCancelled, OrderStatusPendingDispatch},
		{OrderStatusExceptionClosed, OrderStatusDispatching},
	}
	for _, pair := range illegal {
		if CanTransit(pair[0], pair[1]) {
			t.Fatalf("expected transition %d -> %d to be rejected", pair[0], pair[1])
		}
	}
}

func TestTerminalAndActiveStatus(t *testing.T) {
	for _, status := range []OrderStatus{OrderStatusCompleted, OrderStatusCancelled, OrderStatusExceptionClosed} {
		if !status.Terminal() {
			t.Fatalf("status %d should be terminal", status)
		}
		if status.Active() {
			t.Fatalf("status %d should not be active", status)
		}
	}
	for status := OrderStatusPendingDispatch; status <= OrderStatusInTrip; status++ {
		if status.Terminal() {
			t.Fatalf("status %d should not be terminal", status)
		}
		if !status.Active() {
			t.Fatalf("status %d should be active", status)
		}
	}
}

func TestCommandTargetMapping(t *testing.T) {
	tests := []struct {
		name        string
		current     OrderStatus
		commandType string
		want        OrderStatus
		wantErr     error
	}{
		{name: "dispatch pending", current: OrderStatusPendingDispatch, commandType: CommandDispatch, want: OrderStatusDispatching},
		{name: "vehicle accepts", current: OrderStatusDispatching, commandType: CommandDispatchAccept, want: OrderStatusPickingUp},
		{name: "vehicle arrives", current: OrderStatusPickingUp, commandType: CommandMarkArrived, want: OrderStatusWaitingBoarding},
		{name: "start trip", current: OrderStatusWaitingBoarding, commandType: CommandStartTrip, want: OrderStatusInTrip},
		{name: "unlock and start", current: OrderStatusWaitingBoarding, commandType: CommandUnlockAndStart, want: OrderStatusInTrip},
		{name: "complete trip", current: OrderStatusInTrip, commandType: CommandCompleteTrip, want: OrderStatusCompleted},
		{name: "close exception", current: OrderStatusInTrip, commandType: CommandCloseException, want: OrderStatusExceptionClosed},
		{name: "pull over stays", current: OrderStatusInTrip, commandType: CommandRequestPullOver, want: OrderStatusInTrip},
		{name: "reject early arrival", current: OrderStatusPendingDispatch, commandType: CommandMarkArrived, want: OrderStatusPendingDispatch, wantErr: ErrConflict},
		{name: "reject start before arrived", current: OrderStatusPickingUp, commandType: CommandUnlockAndStart, want: OrderStatusPickingUp, wantErr: ErrConflict},
		{name: "reject pull over before trip", current: OrderStatusPickingUp, commandType: CommandRequestPullOver, want: OrderStatusPickingUp, wantErr: ErrConflict},
		{name: "reject unknown", current: OrderStatusPendingDispatch, commandType: "UNKNOWN", want: OrderStatusPendingDispatch, wantErr: ErrInvalidArgument},
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

func TestPassengerCommandWhitelist(t *testing.T) {
	for _, command := range []string{CommandUnlockAndStart, CommandRequestPullOver} {
		if !IsPassengerCommand(command) {
			t.Fatalf("command %s should be allowed for passenger", command)
		}
	}
	for _, command := range []string{CommandDispatch, CommandDispatchAccept, CommandMarkArrived, CommandStartTrip, CommandCompleteTrip, CommandCloseException, CommandCancel} {
		if IsPassengerCommand(command) {
			t.Fatalf("command %s must require internal token", command)
		}
	}
}

func TestStatusPresentationCoversEveryStatus(t *testing.T) {
	seen := map[string]struct{}{}
	for status := OrderStatusPendingDispatch; status <= OrderStatusExceptionClosed; status++ {
		code, text, step, _ := StatusPresentation(status)
		if code == "" || text == "" || step == 0 {
			t.Fatalf("status %d has incomplete presentation: %s/%s/%d", status, code, text, step)
		}
		if _, ok := seen[code]; ok {
			t.Fatalf("duplicated status code %s", code)
		}
		seen[code] = struct{}{}
	}
}

func TestBuildTripProgress(t *testing.T) {
	order := &Order{
		Status: OrderStatusInTrip, DistanceMeters: 20000,
		Destination: Place{Longitude: 114.3, Latitude: 30.5},
	}
	location := &LocationPoint{Longitude: 114.2, Latitude: 30.4, SpeedKMH: 36}
	progress := BuildTripProgress(order, location)
	if progress.RemainingDistanceMeters <= 0 || progress.SpeedKMH != 36 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
	if progress.ProgressPercent <= 0 || progress.ProgressPercent >= 100 {
		t.Fatalf("expected partial progress, actual %d", progress.ProgressPercent)
	}
	if empty := BuildTripProgress(&Order{Status: OrderStatusInTrip}, nil); empty.ProgressPercent != 0 {
		t.Fatalf("expected empty progress without location")
	}
}
