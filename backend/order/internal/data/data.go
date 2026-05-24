package data

import (
	"fmt"
	"order/internal/biz"
	"order/internal/conf"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewGreeterRepo, NewOrderData)

// Data .
type Data struct {
	// TODO wrapped database client
	Mdb *gorm.DB
}

// NewData .
func NewData(c *conf.Data) (*Data, func(), error) {
	databaseConfig := c.GetDatabase()
	if databaseConfig == nil {
		return nil, nil, fmt.Errorf("missing database config")
	}
	if databaseConfig.Driver != "mysql" {
		return nil, nil, fmt.Errorf("unsupported database driver: %s", databaseConfig.Driver)
	}

	data := &Data{}
	var err error
	data.Mdb, err = gorm.Open(mysql.Open(databaseConfig.Source), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := data.Mdb.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	if err := migrateTable(data.Mdb); err != nil {
		sqlDB.Close()
		return nil, nil, err
	}
	cleanup := func() {
		log.Info("closing the data resources")
		sqlDB.Close()
	}
	return data, cleanup, nil
}

func migrateTable(db *gorm.DB) error {
	return db.AutoMigrate(&biz.Order{})
}
