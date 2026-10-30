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

const (
	defaultOutboxRequestTimeout = 2 * time.Second
	defaultOutboxPollInterval   = time.Second
	defaultOutboxLeaseDuration  = 10 * time.Second
)

// DispatchOutboxDispatcher 在应用启动后可靠投递已落库的模块 4 派单指令。
type DispatchOutboxDispatcher struct {
	repository     dispatchOutboxProcessor
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

// NewDispatchOutboxDispatcher 创建可按地址配置启停的后台发送器。
func NewDispatchOutboxDispatcher(c *conf.Data, repository *DispatchRepository, logger log.Logger) *DispatchOutboxDispatcher {
	config := c.GetDispatchOutbox()
	return &DispatchOutboxDispatcher{
		repository: repository,
		address:    config.GetVehicleGatewayAddr(),
		requestTimeout: durationOrDefault(config.GetRequestTimeout(),
			defaultOutboxRequestTimeout),
		pollInterval:  durationOrDefault(config.GetPollInterval(), defaultOutboxPollInterval),
		leaseDuration: durationOrDefault(config.GetLeaseDuration(), defaultOutboxLeaseDuration),
		log:           log.NewHelper(logger),
	}
}

// Enabled 返回是否已配置模块 4 地址；空地址时不建立外部连接。
func (dispatcher *DispatchOutboxDispatcher) Enabled() bool {
	return dispatcher.address != ""
}

// Start 连接模块 4 并启动可取消的投递循环。
func (dispatcher *DispatchOutboxDispatcher) Start(ctx context.Context) error {
	if !dispatcher.Enabled() {
		dispatcher.log.Info("dispatch outbox dispatcher is disabled")
		return nil
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	// AfterStart 可能被重复触发；已有取消函数说明循环已启动，保持幂等。
	if dispatcher.cancel != nil {
		return nil
	}
	connection, err := kratosgrpc.DialInsecure(ctx,
		kratosgrpc.WithEndpoint(dispatcher.address),
		kratosgrpc.WithTimeout(dispatcher.requestTimeout),
		kratosgrpc.WithHealthCheck(false))
	if err != nil {
		return err
	}
	// 使用独立后台上下文，避免启动钩子的短生命周期 ctx 误杀常驻发送循环。
	workerContext, cancel := context.WithCancel(context.Background())
	dispatcher.connection = connection
	dispatcher.cancel = cancel
	dispatcher.done = make(chan struct{})
	go dispatcher.run(workerContext, NewGRPCVehicleDispatchGateway(v1.NewVehicleDispatchGatewayServiceClient(connection)), dispatcher.done)
	return nil
}

// Stop 取消循环、等待发送器退出并关闭模块 4 客户端连接。
func (dispatcher *DispatchOutboxDispatcher) Stop(ctx context.Context) error {
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
	if connection == nil {
		return nil
	}
	return connection.Close()
}

func (dispatcher *DispatchOutboxDispatcher) run(ctx context.Context, gateway biz.VehicleDispatchGateway, done chan struct{}) {
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

type dispatchOutboxProcessor interface {
	ProcessOutboxOnce(context.Context, biz.VehicleDispatchGateway, time.Time, time.Duration) (bool, error)
}

func (dispatcher *DispatchOutboxDispatcher) deliverOnce(ctx context.Context, gateway biz.VehicleDispatchGateway) {
	requestContext, cancel := context.WithTimeout(ctx, dispatcher.requestTimeout)
	defer cancel()
	processed, err := dispatcher.repository.ProcessOutboxOnce(requestContext, gateway, time.Now().UTC(), dispatcher.leaseDuration)
	if err != nil {
		dispatcher.log.Errorf("dispatch outbox delivery failed: %v", err)
		return
	}
	if processed {
		dispatcher.log.Debug("dispatch outbox instruction processed")
	}
}

func durationOrDefault(value interface{ AsDuration() time.Duration }, fallback time.Duration) time.Duration {
	// 空值或非正值统一回退到安全默认值，禁止零间隔忙轮询或零租约重复领取。
	if value == nil || value.AsDuration() <= 0 {
		return fallback
	}
	return value.AsDuration()
}
