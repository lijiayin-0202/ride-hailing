package data

import (
	"context"
	"time"

	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

type configuredOrderQueryGateway struct {
	address string
	timeout time.Duration
}

func NewConfiguredOrderQueryGateway(c *conf.Data) biz.OrderDispatchQueryGateway {
	// 查询网关按配置延迟创建连接，避免服务启动时因模块 2 暂不可用而整体失败。
	config := c.GetDispatchOutbox()
	return &configuredOrderQueryGateway{address: config.GetOrderGatewayAddr(), timeout: durationOrDefault(config.GetRequestTimeout(), defaultOutboxRequestTimeout)}
}

func (gateway *configuredOrderQueryGateway) GetDispatchableOrder(ctx context.Context, orderID int64) (biz.OrderDispatchSnapshot, error) {
	if gateway.address == "" {
		// 未配置共享地址时明确报不可用，禁止退化为本地伪造订单事实。
		return biz.OrderDispatchSnapshot{}, biz.ErrOrderGatewayUnavailable
	}
	connection, err := kratosgrpc.DialInsecure(ctx, kratosgrpc.WithEndpoint(gateway.address), kratosgrpc.WithTimeout(gateway.timeout), kratosgrpc.WithHealthCheck(false))
	if err != nil {
		return biz.OrderDispatchSnapshot{}, biz.ErrOrderGatewayUnavailable
	}
	defer connection.Close()
	return NewOrderDispatchGateway(v1.NewOrderDispatchQueryServiceClient(connection), nil).GetDispatchableOrder(ctx, orderID)
}

var _ biz.OrderDispatchQueryGateway = (*configuredOrderQueryGateway)(nil)
