package data

import (
	"context"
	"customer/internal/biz"
	"customer/internal/conf"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewGreeterRepo, NewCustomerData)

// Data .
type Data struct {
	// TODO wrapped database client
	// 初始化redis客户端
	Rdb *redis.Client
	// 初始化mysql客户端
	Mdb *gorm.DB
}

// NewData .
func NewData(c *conf.Data) (*Data, func(), error) {
	redisConfig := c.GetRedis()
	if redisConfig == nil {
		return nil, nil, fmt.Errorf("missing redis config")
	}
	databaseConfig := c.GetDatabase()
	if databaseConfig == nil {
		return nil, nil, fmt.Errorf("missing database config")
	}
	data := &Data{}
	// 初始化redis客户端
	data.Rdb = redis.NewClient(&redis.Options{
		Addr:         redisConfig.Addr,
		Password:     "",
		DB:           1,
		ReadTimeout:  redisConfig.ReadTimeout.AsDuration(),
		WriteTimeout: redisConfig.WriteTimeout.AsDuration(),
	})
	status := data.Rdb.Ping(context.Background())
	if status.Err() != nil {
		data.Rdb.Close()
		return nil, nil, status.Err()
	}
	if databaseConfig.Driver != "mysql" {
		data.Rdb.Close()
		return nil, nil, fmt.Errorf("unsupported database driver: %s", databaseConfig.Driver)
	}
	// 初始化mysql客户端
	var err error
	data.Mdb, err = gorm.Open(mysql.Open(databaseConfig.Source), &gorm.Config{})
	if err != nil {
		data.Rdb.Close()
		return nil, nil, err
	}
	sqlDB, err := data.Mdb.DB()
	if err != nil {
		data.Rdb.Close()
		return nil, nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	// 自动迁移数据库
	if err := migrateTable(data.Mdb); err != nil {
		data.Rdb.Close()
		sqlDB.Close()
		return nil, nil, err
	}

	cleanup := func() {
		log.Info("closing the data resources")
		data.Rdb.Close()
		sqlDB.Close()
	}
	return data, cleanup, nil
}

func migrateTable(db *gorm.DB) error {
	if err := db.AutoMigrate(&biz.Customer{}); err != nil {
		return err
	}
	return nil
}
