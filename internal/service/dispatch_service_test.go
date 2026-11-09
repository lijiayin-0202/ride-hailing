package service

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
)

func TestAssignOrderRejectsNonPositiveOrderIDWithBadRequest(t *testing.T) {
	service := NewDispatchService(nil)

	_, err := service.AssignOrder(context.Background(), &v1.AssignOrderRequest{OrderId: 0})
	if err == nil {
		t.Fatal("AssignOrder() error = nil, want bad request")
	}
	if got := kratoserrors.Code(err); got != 400 {
		t.Fatalf("AssignOrder() error code = %d, want 400", got)
	}
	if got := kratoserrors.Reason(err); got != "INVALID_DISPATCH_REQUEST" {
		t.Fatalf("AssignOrder() error reason = %q, want INVALID_DISPATCH_REQUEST", got)
	}
}
