package data

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

// ProviderSet 集中管理 PostgreSQL、后台工作者及跨模块适配器的注入关系。
var ProviderSet = wire.NewSet(
	NewData,
	NewDispatchRepository,
	NewServiceAreaRepository,
	NewStationRepository,
	NewVehicleRepository,
	NewDispatchOutboxDispatcher,
	NewOrderDispatchOutboxDispatcher,
	NewDispatchTimeoutWorker,
	NewDispatchReassignmentDispatcher,
	// 订单查询网关以配置方式延迟连接模块 2，供 HTTP 自动派单入口使用。
	NewConfiguredOrderQueryGateway,
	wire.Bind(new(biz.ServiceAreaRepository), new(*ServiceAreaRepository)),
	wire.Bind(new(biz.ServiceAreaAdminRepository), new(*ServiceAreaRepository)),
	wire.Bind(new(biz.StationRepository), new(*StationRepository)),
	wire.Bind(new(biz.VehicleRepository), new(*VehicleRepository)),
	wire.Bind(new(biz.DispatchRecordRepository), new(*DispatchRepository)),
	wire.Bind(new(biz.CandidateVehicleRepository), new(*DispatchRepository)),
	wire.Bind(new(biz.DispatchStateRepository), new(*DispatchRepository)),
	wire.Bind(new(biz.PendingDispatchAttemptRepository), new(*DispatchRepository)),
)

// Data 数据层资源集合（空模板，预留位置）。
//
// 团队接入 MySQL / Redis / MQ 时，把客户端挂到本结构体上，例如：
//
//	type Data struct {
//	    db    *gorm.DB
//	    redis *redis.Client
//	    log   *log.Helper
//	}
type Data struct {
	pool                *pgxpool.Pool
	dispatchIDGenerator biz.DispatchIDGenerator
	log                 *log.Helper
}

// NewData 初始化数据层连接，返回清理函数用于优雅退出。
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(logger)
	if c.GetDatabase().GetDriver() != "postgres" {
		return nil, nil, fmt.Errorf("unsupported database driver: %s", c.GetDatabase().GetDriver())
	}
	if c.GetDatabase().GetSource() == "" {
		return nil, nil, fmt.Errorf("postgres source is required")
	}
	// 节点号由环境变量提供，避免多实例生成碰撞的 BIGINT 派单主键。
	dispatchIDGenerator, err := NewDispatchIDGenerator()
	if err != nil {
		return nil, nil, fmt.Errorf("configure dispatch ID generator: %w", err)
	}

	// 数据源必须由本地忽略配置或部署平台注入，避免把数据库凭据写入版本库。
	pool, err := pgxpool.New(context.Background(), c.GetDatabase().GetSource())
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres pool: %w", err)
	}

	cleanup := func() {
		pool.Close()
		helper.Info("closing the data resources")
	}
	return &Data{pool: pool, dispatchIDGenerator: dispatchIDGenerator, log: helper}, cleanup, nil
}
