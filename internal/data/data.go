package data

import (
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

var ProviderSet = wire.NewSet(NewData, NewRideRepo)

type Data struct {
	db       *gorm.DB
	log      *log.Helper
	phoneKey string

	columnMu    sync.RWMutex
	columnCache map[string]bool
}

func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(logger)
	database := c.GetDatabase()
	if database.GetDriver() != "postgres" || database.GetSource() == "" {
		return nil, nil, fmt.Errorf("postgres database source is required")
	}
	phoneKey := c.GetSecurity().GetContactPhoneKey()
	if len(phoneKey) < 32 {
		return nil, nil, fmt.Errorf("contact phone encryption key must be at least 32 bytes")
	}
	db, err := gorm.Open(postgres.Open(database.GetSource()), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("get postgres connection: %w", err)
	}
	cleanup := func() {
		helper.Info("closing the data resources")
		if err := sqlDB.Close(); err != nil {
			helper.Errorf("close postgres: %v", err)
		}
	}
	return &Data{db: db, log: helper, phoneKey: phoneKey, columnCache: map[string]bool{}}, cleanup, nil
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
	err := d.db.Table("information_schema.columns").
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
