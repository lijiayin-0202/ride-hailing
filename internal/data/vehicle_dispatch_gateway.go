package data

import (
	"context"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// GRPCVehicleDispatchGateway 将领域派单快照映射为模块 4 的 gRPC 指令。
type GRPCVehicleDispatchGateway struct {
	client v1.VehicleDispatchGatewayServiceClient
}

// NewGRPCVehicleDispatchGateway 创建模块 4 派单指令客户端适配器。
func NewGRPCVehicleDispatchGateway(client v1.VehicleDispatchGatewayServiceClient) *GRPCVehicleDispatchGateway {
	return &GRPCVehicleDispatchGateway{client: client}
}

// Send 下发一条具备稳定幂等键的派单指令。
func (gateway *GRPCVehicleDispatchGateway) Send(ctx context.Context, input biz.VehicleDispatchInstruction) (bool, error) {
	// dispatch_no 固定作为模块 4 的幂等键；Outbox 重试不会要求车辆端重复创建任务。
	reply, err := gateway.client.SendDispatchInstruction(ctx, &v1.SendDispatchInstructionRequest{
		Instruction: &v1.DispatchInstruction{
			IdempotencyKey: input.Dispatch.DispatchNo, DispatchId: input.Dispatch.ID,
			DispatchNo: input.Dispatch.DispatchNo, OrderId: input.Dispatch.OrderID,
			VehicleId: input.Dispatch.VehicleID, AttemptNo: int32(input.Dispatch.AttemptNo),
			Pickup: dispatchGeoPoint(input.Pickup), Dropoff: dispatchGeoPoint(input.Dropoff),
			ConfirmationDeadlineUnixMilli: input.ConfirmationDeadline.UnixMilli(),
		},
	})
	if err != nil {
		return false, err
	}
	return reply.GetAcceptedForDelivery(), nil
}

func dispatchGeoPoint(location biz.DispatchLocation) *v1.GeoPoint {
	return &v1.GeoPoint{StationId: location.StationID, Longitude: location.Longitude, Latitude: location.Latitude, Address: location.Address}
}

var _ biz.VehicleDispatchGateway = (*GRPCVehicleDispatchGateway)(nil)
