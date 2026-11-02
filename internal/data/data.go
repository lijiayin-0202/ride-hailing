package data

import (
	"fmt"

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
	return &Data{db: db, log: helper, phoneKey: phoneKey}, cleanup, nil
}
