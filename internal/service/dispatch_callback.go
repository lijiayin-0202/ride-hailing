package service

import (
	"context"
	"errors"
	"strings"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

var errInvalidDispatchCallback = errors.New("派单车辆回调无效")

// DispatchCallbackApplication 描述派单结果的领域入口。
type DispatchCallbackApplication interface {
	Resolve(context.Context, biz.DispatchResolution) (biz.DispatchRecord, error)
}

// DispatchCallbackService 接收模块 4 的车辆确认或拒绝事实。
type DispatchCallbackService struct {
	v1.UnimplementedDispatchCallbackServiceServer
	application DispatchCallbackApplication
}

// NewDispatchCallbackService 创建派单车辆回调服务。
func NewDispatchCallbackService(application DispatchCallbackApplication) *DispatchCallbackService {
	return &DispatchCallbackService{application: application}
}

// ReportVehicleDispatchResponse 处理车辆确认或拒绝回调。
func (service *DispatchCallbackService) ReportVehicleDispatchResponse(ctx context.Context, request *v1.ReportVehicleDispatchResponseRequest) (*v1.ReportVehicleDispatchResponseReply, error) {
	resolution, err := callbackResolution(request)
	if err != nil {
		return nil, err
	}
	// 领域层以条件更新保证同一回调重复到达时不会重复触发补派。
	if _, err := service.application.Resolve(ctx, resolution); err != nil {
		return nil, err
	}
	return &v1.ReportVehicleDispatchResponseReply{Accepted: true}, nil
}

func callbackResolution(request *v1.ReportVehicleDispatchResponseRequest) (biz.DispatchResolution, error) {
	// 回调必须携带完整事实，避免仅凭 dispatch_id 接受来自错误订单或错误车辆的响应。
	if request == nil || strings.TrimSpace(request.GetIdempotencyKey()) == "" || request.GetDispatchId() <= 0 || request.GetOrderId() <= 0 || request.GetVehicleId() <= 0 || request.GetRespondedAtUnixMilli() <= 0 {
		return biz.DispatchResolution{}, errInvalidDispatchCallback
	}
	if request.GetResponseType() == v1.VehicleDispatchResponseType_VEHICLE_DISPATCH_RESPONSE_TYPE_ACCEPTED {
		return biz.DispatchResolution{DispatchID: request.GetDispatchId(), OrderID: request.GetOrderId(), VehicleID: request.GetVehicleId(), Status: biz.DispatchStatusAccepted, RespondedAtUnixMilli: request.GetRespondedAtUnixMilli()}, nil
	}
	if request.GetResponseType() != v1.VehicleDispatchResponseType_VEHICLE_DISPATCH_RESPONSE_TYPE_REJECTED || strings.TrimSpace(request.GetRejectionReason()) == "" {
		return biz.DispatchResolution{}, errInvalidDispatchCallback
	}
	return biz.DispatchResolution{DispatchID: request.GetDispatchId(), OrderID: request.GetOrderId(), VehicleID: request.GetVehicleId(), Status: biz.DispatchStatusRejected, FailureReason: request.GetRejectionReason(), RespondedAtUnixMilli: request.GetRespondedAtUnixMilli()}, nil
}

var _ v1.DispatchCallbackServiceServer = (*DispatchCallbackService)(nil)
