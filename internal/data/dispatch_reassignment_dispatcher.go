package data

import (
	"context"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

type reassignmentProcessor interface {
	ProcessReassignmentOnce(context.Context, ReassignmentApplication, time.Time, time.Duration) (bool, error)
}

// DispatchReassignmentDispatcher 周期领取拒绝或超时后的补派任务。
type DispatchReassignmentDispatcher struct {
	processor     reassignmentProcessor
	application   ReassignmentApplication
	pollInterval  time.Duration
	leaseDuration time.Duration
	log           *log.Helper

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewDispatchReassignmentDispatcher 创建由 Kratos 生命周期管理的补派工作者。
func NewDispatchReassignmentDispatcher(c *conf.Data, repository *DispatchRepository, application *biz.AutoDispatchUsecase, logger log.Logger) *DispatchReassignmentDispatcher {
	config := c.GetDispatchOutbox()
	return newDispatchReassignmentDispatcher(repository, application,
		durationOrDefault(config.GetReassignmentPollInterval(), time.Second), logger,
		durationOrDefault(config.GetLeaseDuration(), defaultOutboxLeaseDuration))
}

func newDispatchReassignmentDispatcher(processor reassignmentProcessor, application ReassignmentApplication, pollInterval time.Duration, logger log.Logger, leaseDuration ...time.Duration) *DispatchReassignmentDispatcher {
	// 可变参数仅供单测注入较短租约；生产构造函数固定从配置读取。
	lease := defaultOutboxLeaseDuration
	if len(leaseDuration) == 1 {
		lease = leaseDuration[0]
	}
	return &DispatchReassignmentDispatcher{processor: processor, application: application, pollInterval: pollInterval, leaseDuration: lease, log: log.NewHelper(logger)}
}

// Start 启动可取消的补派循环。
func (dispatcher *DispatchReassignmentDispatcher) Start(context.Context) error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.cancel != nil {
		return nil
	}
	// 使用独立后台上下文，避免 Kratos AfterStart 调用返回后工作循环被提前取消。
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher.cancel, dispatcher.done = cancel, make(chan struct{})
	go dispatcher.run(ctx, dispatcher.done)
	return nil
}

// Stop 取消循环并等待其退出。
func (dispatcher *DispatchReassignmentDispatcher) Stop(ctx context.Context) error {
	dispatcher.mu.Lock()
	if dispatcher.cancel == nil {
		dispatcher.mu.Unlock()
		return nil
	}
	cancel, done := dispatcher.cancel, dispatcher.done
	dispatcher.cancel, dispatcher.done = nil, nil
	dispatcher.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (dispatcher *DispatchReassignmentDispatcher) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(dispatcher.pollInterval)
	defer ticker.Stop()
	for {
		dispatcher.processOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (dispatcher *DispatchReassignmentDispatcher) processOnce(ctx context.Context) {
	// 单轮最多领取一个任务，公平地让出循环并由下一轮继续领取。
	_, err := dispatcher.processor.ProcessReassignmentOnce(ctx, dispatcher.application, time.Now().UTC(), dispatcher.leaseDuration)
	if err != nil && ctx.Err() == nil {
		dispatcher.log.Errorf("process dispatch reassignment: %v", err)
	}
}
