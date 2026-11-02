package data

import (
	"context"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

type timeoutExpirer interface {
	ExpireTimeouts(context.Context) (int, error)
}

// DispatchTimeoutWorker 周期扫描超过 30 秒未确认的派单。
type DispatchTimeoutWorker struct {
	expirer      timeoutExpirer
	pollInterval time.Duration
	log          *log.Helper
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
}

func NewDispatchTimeoutWorker(c *conf.Data, expirer *biz.DispatchStateUsecase, logger log.Logger) *DispatchTimeoutWorker {
	// 复用调度轮询频率即可；派单的业务确认时限由领域层固定为 30 秒。
	config := c.GetDispatchOutbox()
	return &DispatchTimeoutWorker{expirer: expirer, pollInterval: durationOrDefault(config.GetReassignmentPollInterval(), time.Second), log: log.NewHelper(logger)}
}

func (worker *DispatchTimeoutWorker) Start(context.Context) error {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.cancel != nil {
		return nil
	}
	// 超时扫描独立运行，应用关闭时由 Stop 统一取消。
	ctx, cancel := context.WithCancel(context.Background())
	worker.cancel, worker.done = cancel, make(chan struct{})
	go worker.run(ctx, worker.done)
	return nil
}

func (worker *DispatchTimeoutWorker) Stop(ctx context.Context) error {
	worker.mu.Lock()
	if worker.cancel == nil {
		worker.mu.Unlock()
		return nil
	}
	cancel, done := worker.cancel, worker.done
	worker.cancel, worker.done = nil, nil
	worker.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (worker *DispatchTimeoutWorker) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(worker.pollInterval)
	defer ticker.Stop()
	for {
		// 每轮将超过 30 秒的 Pending 派单转为 TIMED_OUT，并触发同一补派事务。
		if _, err := worker.expirer.ExpireTimeouts(ctx); err != nil && ctx.Err() == nil {
			worker.log.Errorf("expire dispatch timeouts: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
