// Package cache 收敛 Redis 客户端构造，供微服务各模块统一复用。
//
// 为什么单独成包（架构约束《1.1 技术与架构约束》"缓存：Redis，沿用项目既有封装"）：
//  1. 后台服务 radish-admin-server 的封装是 initialize/redis.go + global.GVA_REDIS（进程内全局）；
//     ride-hailing 是独立进程、独立 go module，无法直接引用其全局变量，
//     因此这里沿用同一套构造口径：同版本 go-redis、单机/集群统一用 UniversalClient、
//     超时与连接参数集中声明，避免各模块各自 new 一个 client。
//  2. 连接不可用时不阻断启动：调用方拿到 client 后自行 Ping，失败则降级（验证码退化为进程内存储）。
package cache

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// 默认超时：与后台服务 initialize/redis.go 的「本地缓存不拖垮主流程」原则一致。
const (
	// DefaultDialTimeout 建连超时。
	DefaultDialTimeout = 2 * time.Second
	// DefaultReadTimeout 读超时。
	DefaultReadTimeout = 500 * time.Millisecond
	// DefaultWriteTimeout 写超时。
	DefaultWriteTimeout = 500 * time.Millisecond
)

// Config Redis 连接参数（与 GVA 的 config.Redis 字段口径对齐）。
type Config struct {
	// Addr 单机地址，如 127.0.0.1:6379。
	Addr string
	// ClusterAddrs 集群节点地址；UseCluster 为 true 时生效。
	ClusterAddrs []string
	// UseCluster 是否使用集群模式。
	UseCluster bool
	// Password 连接口令，未设置时为空。
	Password string
	// DB 逻辑库编号，仅单机模式生效。
	DB int
	// DialTimeout 建连超时，未设置时使用 DefaultDialTimeout。
	DialTimeout time.Duration
	// ReadTimeout 读超时，未设置时使用 DefaultReadTimeout。
	ReadTimeout time.Duration
	// WriteTimeout 写超时，未设置时使用 DefaultWriteTimeout。
	WriteTimeout time.Duration
}

// New 构造 Redis 客户端；单机与集群都返回 UniversalClient，调用方不需要区分类型。
func New(cfg Config) redis.UniversalClient {
	dialTimeout := orDefault(cfg.DialTimeout, DefaultDialTimeout)
	readTimeout := orDefault(cfg.ReadTimeout, DefaultReadTimeout)
	writeTimeout := orDefault(cfg.WriteTimeout, DefaultWriteTimeout)

	if cfg.UseCluster {
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.ClusterAddrs,
			Password:     cfg.Password,
			DialTimeout:  dialTimeout,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		})
	}
	return redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	})
}

// orDefault 零值或负值按默认值处理，避免 conn 立即超时。
func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}
