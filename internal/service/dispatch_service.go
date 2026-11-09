package service

import (
	"context"
	stderrors "errors"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var errInvalidDispatchRequest = stderrors.New("派单订单参数无效")

type DispatchApplication interface {
	// HTTP/gRPC 服务只传订单 ID，选车、幂等及派单持久化均由领域层负责。
	Assign(context.Context, int64) (biz.AssignDispatchResult, error)
}

type DispatchService struct {
	v1.UnimplementedDispatchServiceServer
	application DispatchApplication
}

func NewDispatchService(application DispatchApplication) *DispatchService {
	return &DispatchService{application: application}
}

func (service *DispatchService) AssignOrder(ctx context.Context, request *v1.AssignOrderRequest) (*v1.AssignOrderReply, error) {
	if request == nil || request.GetOrderId() <= 0 {
		return nil, kratoserrors.BadRequest("INVALID_DISPATCH_REQUEST", errInvalidDispatchRequest.Error())
	}
	// 不接受 vehicle_id，保证候选排序规则不会被调用方绕过。
	result, err := service.application.Assign(ctx, request.GetOrderId())
	if err != nil {
		return nil, dispatchServiceError(err)
	}
	record := result.Record
	return &v1.AssignOrderReply{DispatchId: record.ID, DispatchNo: record.DispatchNo, OrderId: record.OrderID, VehicleId: record.VehicleID,
		AttemptNo: int32(record.AttemptNo), DispatchStrategy: record.DispatchStrategy, CandidateScore: record.CandidateScore,
		EstimatedPickupDistanceKm: record.EstimatedPickupDistanceKM, EstimatedArrivalMin: record.EstimatedArrivalMin,
		Status: int32(record.Status), ConfirmationDeadlineUnixMilli: result.ConfirmationDeadline.UnixMilli(), ReusedActiveAttempt: result.ReusedActiveAttempt}, nil
}

func dispatchServiceError(err error) error {
	if stderrors.Is(err, biz.ErrOrderGatewayUnavailable) {
		return kratoserrors.ServiceUnavailable("ORDER_GATEWAY_UNAVAILABLE", err.Error())
	}
	if stderrors.Is(err, biz.ErrOrderNotFound) || stderrors.Is(err, biz.ErrOrderNotSchedulable) {
		return kratoserrors.BadRequest("ORDER_NOT_SCHEDULABLE", err.Error())
	}
	return err
}
