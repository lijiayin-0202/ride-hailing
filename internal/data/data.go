// 数据访问层（repo）。
// 模块1 用户账号与个人中心（成员A）接入 PostgreSQL 主库与 Redis 缓存，
// 其余模块的 repo 请各自新增文件，不要复用本文件的连接初始化逻辑。
package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,

	// 模块1 用户账号与个人中心（成员A）
	NewPassengerRepo,
	NewPassengerAuthRecordRepo,
	NewAddressRepo,
	NewCouponRepo,
	NewPointRepo,
	NewNotificationRepo,
	NewRealNameRepo,
	NewLoginDeviceRepo,
	NewLoginRecordRepo,
	NewSmsCodeStore,
	NewSmsSender,
	NewTokenIssuer,
)

// Data 数据层资源集合。
type Data struct {
	// db PostgreSQL 连接池（主库，AGENT.md §2 固定 PostgreSQL）。
	db *pgxpool.Pool
	// rdb Redis 客户端；不可用时为 nil，验证码退化为进程内存储。
	rdb *redis.Client
	// log 结构化日志。
	log *log.Helper
}

// NewData 初始化数据层连接，返回清理函数用于优雅退出。
//
// 连接串来源优先级：环境变量 RIDE_DATABASE_SOURCE > 配置文件 data.database.source。
// 主库固定 PostgreSQL（AGENT.md §2），驱动名不是 postgres 时直接失败，避免误连 MySQL。
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(log.With(logger, "module", "data"))

	driver := strings.ToLower(strings.TrimSpace(c.GetDatabase().GetDriver()))
	if driver != "" && driver != "postgres" && driver != "postgresql" && driver != "pgx" {
		return nil, nil, fmt.Errorf("不支持的数据库驱动 %q，本项目主库固定为 PostgreSQL", driver)
	}

	// 配置读取唯一入口：internal/conf（.agent/04 §2.1，业务层禁止裸用 os.Getenv）。
	// yaml 中只保留 ${RIDE_DATABASE_SOURCE} 占位，由 conf 统一展开。
	dsn := conf.DatabaseSource()
	if dsn == "" {
		dsn = conf.ExpandEnvPlaceholder(c.GetDatabase().GetSource())
	}
	if dsn == "" {
		return nil, nil, fmt.Errorf("数据库连接串为空：请注入环境变量 %s（configs/config.yaml 仅保留 ${%s} 占位）",
			conf.EnvDatabaseSource, conf.EnvDatabaseSource)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("解析数据库连接串失败: %w", err)
	}
	// 连接池参数：并发低的开发环境也至少保留 8 个连接，避免登录高峰排队。
	if poolCfg.MaxConns < 4 {
		poolCfg.MaxConns = 8
	}
	// 连接最长存活 1 小时，规避服务端空闲断连；每分钟健康检查一次。
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("创建数据库连接池失败: %w", err)
	}
	// 启动即 Ping：连接不可用直接在启动阶段失败，避免"起来了但全量接口 500"。
	// 说明：NewData 由 wire 在进程初始化阶段调用，尚无请求上下文，故使用独立超时上下文。
	pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("数据库连接不可用: %w", err)
	}

	d := &Data{db: pool, log: helper}

	// Redis：验证码短期存储与限流使用。连接不可用时不阻断启动，
	// 由 NewSmsCodeStore 退化为进程内存储并打印告警（仅限单实例开发/联调）。
	if addr := strings.TrimSpace(c.GetRedis().GetAddr()); addr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:         addr,
			DialTimeout:  2 * time.Second,
			ReadTimeout:  durationOrDefault(c.GetRedis().GetReadTimeout().AsDuration(), 500*time.Millisecond),
			WriteTimeout: durationOrDefault(c.GetRedis().GetWriteTimeout().AsDuration(), 500*time.Millisecond),
		})
		redisCtx, redisCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rdb.Ping(redisCtx).Err(); err != nil {
			helper.Warnf("Redis 不可用（%s），验证码将使用进程内存储: %v", addr, err)
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
		pool.Close()
		helper.Info("closing the data resources")
	}
	return d, cleanup, nil
}

func durationOrDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// 编译期断言：确保 data 层实现 biz 定义的仓库接口。
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
)
