package data

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData)

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
	log *log.Helper
}

// NewData 初始化数据层连接，返回清理函数用于优雅退出。
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(logger)

	// TODO(团队): 依据 c.GetDatabase() / c.GetRedis() 建立连接

	cleanup := func() {
		// TODO(团队): 关闭 db / redis 等资源
		helper.Info("closing the data resources")
	}
	return &Data{log: helper}, cleanup, nil
}
