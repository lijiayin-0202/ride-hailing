// 数据访问层（repo）。
package data

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/pkg/cache"
)

// ProviderSet 集中管理模块1、模块3 与订单模块的数据层实现。
var ProviderSet = wire.NewSet(
	NewData,
	// 订单、报价、支付与派车记录（GORM 实现）。
	NewRideRepo,
	// 模块1 用户账号与个人中心。
	NewPassengerRepo,
	NewPassengerAuthRecordRepo,
	NewAddressRepo,
	NewCouponRepo,
	NewPointRepo,
	NewNotificationRepo,
	NewRealNameRepo,
	NewLoginDeviceRepo,
	NewLoginRecordRepo,
	NewEmergencyContactRepo,
	NewSmsCodeStore,
	NewSmsSender,
	NewTokenIssuer,
	// 模块3 运营区域、站点与派单。
	NewDispatchRepository,
	NewServiceAreaRepository,
	NewStationRepository,
	NewVehicleRepository,
	NewDispatchOutboxDispatcher,
	NewOrderDispatchOutboxDispatcher,
	NewDispatchTimeoutWorker,
	NewDispatchReassignmentDispatcher,
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

// Data 保存共享的 PostgreSQL、Redis 与派单 ID 生成资源。
type Data struct {
	// db 供模块1仓储使用；pool 供模块3仓储使用，二者指向同一连接池。
	db                  *pgxpool.Pool
	pool                *pgxpool.Pool
	dispatchIDGenerator biz.DispatchIDGenerator
	rdb                 redis.UniversalClient
	log                 *log.Helper

	// gormDB 供订单/报价/支付等 GORM 仓储使用，与 pool 指向同一个数据库。
	gormDB   *gorm.DB
	phoneKey string

	columnMu    sync.RWMutex
	columnCache map[string]bool
}

// NewData 初始化数据层连接，返回清理函数用于优雅退出。
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(log.With(logger, "module", "data"))

	driver := strings.ToLower(strings.TrimSpace(c.GetDatabase().GetDriver()))
	if driver != "" && driver != "postgres" && driver != "postgresql" && driver != "pgx" {
		return nil, nil, fmt.Errorf("不支持的数据库驱动 %q，本项目主库固定为 PostgreSQL", driver)
	}

	dsn := conf.DatabaseSource()
	if dsn == "" {
		dsn = conf.ExpandEnvPlaceholder(c.GetDatabase().GetSource())
	}
	if dsn == "" {
		return nil, nil, fmt.Errorf("数据库连接串为空：请注入环境变量 %s（configs/config.yaml 仅保留 ${%s} 占位）",
			conf.EnvDatabaseSource, conf.EnvDatabaseSource)
	}

	phoneKey := c.GetSecurity().GetContactPhoneKey()
	if len(phoneKey) < 32 {
		return nil, nil, fmt.Errorf("联系人手机号加密密钥至少需要 32 字节：请注入环境变量 RIDE_DATA_SECURITY_CONTACT_PHONE_KEY")
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("解析数据库连接串失败: %w", err)
	}
	if poolCfg.MaxConns < 4 {
		poolCfg.MaxConns = 8
	}
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("创建数据库连接池失败: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("数据库连接不可用: %w", err)
	}

	// 订单模块的仓储基于 GORM，复用同一个 DSN 建立独立连接池。
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("初始化 GORM 连接失败: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("获取 GORM 底层连接失败: %w", err)
	}
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetConnMaxLifetime(time.Hour)

	dispatchIDGenerator, err := NewDispatchIDGenerator()
	if err != nil {
		_ = sqlDB.Close()
		pool.Close()
		return nil, nil, fmt.Errorf("配置派单 ID 生成器失败: %w", err)
	}

	d := &Data{
		db:                  pool,
		pool:                pool,
		dispatchIDGenerator: dispatchIDGenerator,
		log:                 helper,
		gormDB:              gormDB,
		phoneKey:            phoneKey,
		columnCache:         map[string]bool{},
	}

	redisAddr := strings.TrimSpace(c.GetRedis().GetAddr())
	useCluster := c.GetRedis().GetUseCluster()
	clusterAddrs := c.GetRedis().GetClusterAddrs()
	if redisAddr != "" || (useCluster && len(clusterAddrs) > 0) {
		rdb := cache.New(cache.Config{
			Addr:         redisAddr,
			ClusterAddrs: clusterAddrs,
			UseCluster:   useCluster,
			Password:     c.GetRedis().GetPassword(),
			DB:           int(c.GetRedis().GetDb()),
			ReadTimeout:  c.GetRedis().GetReadTimeout().AsDuration(),
			WriteTimeout: c.GetRedis().GetWriteTimeout().AsDuration(),
		})
		redisCtx, redisCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rdb.Ping(redisCtx).Err(); err != nil {
			helper.Warnf("Redis 不可用（addr=%q cluster=%v），验证码将使用进程内存储: %v", redisAddr, clusterAddrs, err)
			_ = rdb.Close()
		} else {
			d.rdb = rdb
		}
		redisCancel()
	}

	cleanup := func() {
		if d.rdb != nil {
			_ = d.rdb.Close()
		}
		if err := sqlDB.Close(); err != nil {
			helper.Errorf("关闭 GORM 连接失败: %v", err)
		}
		pool.Close()
		helper.Info("closing the data resources")
	}
	return d, cleanup, nil
}

// hasColumn 探测表字段是否存在。跨模块表（vehicles / dispatch_records 等）的字段名
// 尚未由对应模块冻结，探测后再拼查询可以避免试算整体失败。
func (d *Data) hasColumn(table, column string) bool {
	key := table + "." + column
	d.columnMu.RLock()
	value, ok := d.columnCache[key]
	d.columnMu.RUnlock()
	if ok {
		return value
	}
	var count int64
	err := d.gormDB.Table("information_schema.columns").
		Where("table_name = ? AND column_name = ?", table, column).
		Count(&count).Error
	exists := err == nil && count > 0
	d.columnMu.Lock()
	d.columnCache[key] = exists
	d.columnMu.Unlock()
	return exists
}

// hasTable 探测表是否存在，用于跨模块只读聚合的降级处理。
func (d *Data) hasTable(table string) bool {
	return d.hasColumn(table, "id")
}

// 编译期断言：确保模块1 data 层实现 biz 定义的仓库接口。
var (
	_ biz.PassengerRepo           = (*passengerRepo)(nil)
	_ biz.PassengerAuthRecordRepo = (*passengerAuthRecordRepo)(nil)
	_ biz.AddressRepo             = (*addressRepo)(nil)
	_ biz.CouponRepo              = (*couponRepo)(nil)
	_ biz.PointRepo               = (*pointRepo)(nil)
	_ biz.NotificationRepo        = (*notificationRepo)(nil)
	_ biz.RealNameRepo            = (*realNameRepo)(nil)
	_ biz.LoginDeviceRepo         = (*loginDeviceRepo)(nil)
	_ biz.LoginRecordRepo         = (*loginRecordRepo)(nil)
	_ biz.EmergencyContactRepo    = (*emergencyContactRepo)(nil)
)
