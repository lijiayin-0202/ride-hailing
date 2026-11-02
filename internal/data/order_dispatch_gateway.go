package data

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OrderDispatchGateway 将模块 2 的查询和状态同步契约转换为领域端口。
type OrderDispatchGateway struct {
	query v1.OrderDispatchQueryServiceClient
	sync  v1.OrderDispatchSyncServiceClient
}

// NewOrderDispatchGateway 创建模块 2 gRPC 适配器。
func NewOrderDispatchGateway(query v1.OrderDispatchQueryServiceClient, sync v1.OrderDispatchSyncServiceClient) *OrderDispatchGateway {
	return &OrderDispatchGateway{query: query, sync: sync}
}

// GetDispatchableOrder 查询并转换模块 2 返回的订单派单快照。
func (gateway *OrderDispatchGateway) GetDispatchableOrder(ctx context.Context, orderID int64) (biz.OrderDispatchSnapshot, error) {
	// 适配层仅做协议转换，领域层只依赖端口，不依赖 protobuf 或 gRPC 状态码。
	reply, err := gateway.query.GetDispatchableOrder(ctx, &v1.GetDispatchableOrderRequest{OrderId: orderID})
	if err != nil {
		return biz.OrderDispatchSnapshot{}, mapOrderGatewayError("get dispatchable order", err)
	}
	return orderDispatchSnapshot(reply), nil
}

// Sync 通过稳定派单编号向模块 2 同步状态事实。
func (gateway *OrderDispatchGateway) Sync(ctx context.Context, event biz.OrderDispatchEvent) (bool, error) {
	// 使用派单编号作为幂等键：Outbox 网络重试可安全重复调用模块 2。
	reply, err := gateway.sync.SyncDispatchState(ctx, &v1.SyncOrderDispatchStateRequest{
		IdempotencyKey: event.DispatchNo, OrderId: event.OrderID, DispatchId: event.DispatchID,
		VehicleId: event.VehicleID, EventType: orderDispatchEventType(event.Type), Reason: event.Reason,
		OccurredAtUnixMilli: event.OccurredAt.UnixMilli(),
	})
	if err != nil {
		return false, mapOrderGatewayError("sync dispatch state", err)
	}
	return reply.GetAccepted(), nil
}

func orderDispatchSnapshot(reply *v1.GetDispatchableOrderReply) biz.OrderDispatchSnapshot {
	return biz.OrderDispatchSnapshot{
		OrderID: reply.GetOrderId(), ServiceAreaID: reply.GetServiceAreaId(), OrderStatus: reply.GetOrderStatus(),
		Pickup: dispatchLocation(reply.GetPickup()), Dropoff: dispatchLocation(reply.GetDropoff()),
		Cancelled: reply.GetIsCancelled(), Schedulable: reply.GetIsSchedulable(),
		ReservedAt: time.UnixMilli(reply.GetReservedAtUnixMilli()).UTC(),
	}
}

func dispatchLocation(point *v1.GeoPoint) biz.DispatchLocation {
	return biz.DispatchLocation{StationID: point.GetStationId(), Longitude: point.GetLongitude(), Latitude: point.GetLatitude(), Address: point.GetAddress()}
}

func mapOrderGatewayError(operation string, err error) error {
	// 仅归一化上层可恢复的错误；其余错误保留原始原因便于排障。
	code := status.Code(err)
	if code == codes.NotFound {
		return fmt.Errorf("%s: %w", operation, biz.ErrOrderNotFound)
	}
	if code == codes.Unavailable || code == codes.DeadlineExceeded {
		return fmt.Errorf("%s: %w", operation, biz.ErrOrderGatewayUnavailable)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func orderDispatchEventType(eventType biz.OrderDispatchEventType) v1.OrderDispatchEventType {
	return v1.OrderDispatchEventType(eventType)
}

var _ biz.OrderDispatchQueryGateway = (*OrderDispatchGateway)(nil)
var _ biz.OrderDispatchSyncGateway = (*OrderDispatchGateway)(nil)
