package data

import (
	"fmt"
	"time"

	"valuation/internal/biz"
	"valuation/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewGreeterRepo, NewPriceRuleInterface)

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
	if err := db.AutoMigrate(&biz.PriceRule{}); err != nil {
		return err
	}

	priceRules := []biz.PriceRule{
		{
			PriceRuleWork: biz.PriceRuleWork{
				CityId:      1,
				StartFee:    300,
				DistanceFee: 35,
				DurationFee: 10,
				StartTime:   7,
				EndTime:     23,
			},
		},
		{
			PriceRuleWork: biz.PriceRuleWork{
				CityId:      1,
				StartFee:    350,
				DistanceFee: 35,
				DurationFee: 10,
				StartTime:   23,
				EndTime:     24,
			},
		},
		{
			PriceRuleWork: biz.PriceRuleWork{
				CityId:      1,
				StartFee:    450,
				DistanceFee: 35,
				DurationFee: 10,
				StartTime:   0,
				EndTime:     7,
			},
		},
	}
	for _, priceRule := range priceRules {
		err := db.Where(
			"city_id = ? AND start_time = ? AND end_time = ?",
			priceRule.CityId,
			priceRule.StartTime,
			priceRule.EndTime,
		).FirstOrCreate(&priceRule).Error
		if err != nil {
			return err
		}
	}
	return nil
}
