package data

import (
	"context"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

// OrderDispatchOutboxDispatcher 将已落库的派单状态事件可靠投递给模块 2。
type OrderDispatchOutboxDispatcher struct {
	repository     orderDispatchOutboxProcessor
	address        string
	requestTimeout time.Duration
	pollInterval   time.Duration
	leaseDuration  time.Duration
	log            *log.Helper

	mu         sync.Mutex
	connection *grpc.ClientConn
	cancel     context.CancelFunc
	done       chan struct{}
}

type orderDispatchOutboxProcessor interface {
	ProcessOrderDispatchOutboxOnce(context.Context, biz.OrderDispatchSyncGateway, time.Time, time.Duration) (bool, error)
}

// NewOrderDispatchOutboxDispatcher 创建可按模块 2 地址启停的状态发送器。
func NewOrderDispatchOutboxDispatcher(c *conf.Data, repository *DispatchRepository, logger log.Logger) *OrderDispatchOutboxDispatcher {
	config := c.GetDispatchOutbox()
	return &OrderDispatchOutboxDispatcher{
		repository: repository, address: config.GetOrderGatewayAddr(), log: log.NewHelper(logger),
		requestTimeout: durationOrDefault(config.GetRequestTimeout(), defaultOutboxRequestTimeout),
		pollInterval:   durationOrDefault(config.GetOrderSyncPollInterval(), defaultOutboxPollInterval),
		leaseDuration:  durationOrDefault(config.GetLeaseDuration(), defaultOutboxLeaseDuration),
	}
}

// Enabled 返回模块 2 地址是否已配置。
func (dispatcher *OrderDispatchOutboxDispatcher) Enabled() bool { return dispatcher.address != "" }

// Start 建立模块 2 客户端并启动可取消投递循环。
func (dispatcher *OrderDispatchOutboxDispatcher) Start(ctx context.Context) error {
	// 未配置团队模块 2 地址时保持发送器静默，但数据库 Outbox 仍会持续积压等待恢复。
	if !dispatcher.Enabled() {
		dispatcher.log.Info("order dispatch outbox dispatcher is disabled")
		return nil
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.cancel != nil {
		return nil
	}
	connection, err := kratosgrpc.DialInsecure(ctx, kratosgrpc.WithEndpoint(dispatcher.address),
		kratosgrpc.WithTimeout(dispatcher.requestTimeout), kratosgrpc.WithHealthCheck(false))
	if err != nil {
		return err
	}
	workerContext, cancel := context.WithCancel(context.Background())
	dispatcher.connection, dispatcher.cancel, dispatcher.done = connection, cancel, make(chan struct{})
	// 状态同步只使用模块 2 Sync 客户端；查询客户端在 HTTP 派单入口按请求建立。
	go dispatcher.run(workerContext, NewOrderDispatchGateway(nil, v1.NewOrderDispatchSyncServiceClient(connection)), dispatcher.done)
	return nil
}

// Stop 取消循环、等待退出并关闭客户端连接。
func (dispatcher *OrderDispatchOutboxDispatcher) Stop(ctx context.Context) error {
	dispatcher.mu.Lock()
	if dispatcher.cancel == nil {
		dispatcher.mu.Unlock()
		return nil
	}
	cancel, done, connection := dispatcher.cancel, dispatcher.done, dispatcher.connection
	dispatcher.cancel, dispatcher.done, dispatcher.connection = nil, nil, nil
	dispatcher.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	// Start 成功后必定保存连接；关闭放在循环退出后，避免进行中的 gRPC 被抢先打断。
	return connection.Close()
}

func (dispatcher *OrderDispatchOutboxDispatcher) run(ctx context.Context, gateway biz.OrderDispatchSyncGateway, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(dispatcher.pollInterval)
	defer ticker.Stop()
	for {
		dispatcher.deliverOnce(ctx, gateway)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (dispatcher *OrderDispatchOutboxDispatcher) deliverOnce(ctx context.Context, gateway biz.OrderDispatchSyncGateway) {
	requestContext, cancel := context.WithTimeout(ctx, dispatcher.requestTimeout)
	defer cancel()
	processed, err := dispatcher.repository.ProcessOrderDispatchOutboxOnce(requestContext, gateway, time.Now().UTC(), dispatcher.leaseDuration)
	if err != nil {
		dispatcher.log.Errorf("order dispatch outbox delivery failed: %v", err)
	} else if processed {
		dispatcher.log.Debug("order dispatch outbox event processed")
	}
}
