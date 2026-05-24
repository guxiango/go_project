package data

import (
	"admin/internal/biz"
	"admin/internal/conf"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewGreeterRepo, NewAdminRepo)

// Data .
type Data struct {
	Mdb *gorm.DB
}

// NewData .
func NewData(c *conf.Data, security *conf.Security) (*Data, func(), error) {
	databaseConfig := c.GetDatabase()
	if databaseConfig == nil {
		return nil, nil, fmt.Errorf("missing database config")
	}
	if databaseConfig.Driver != "mysql" {
		return nil, nil, fmt.Errorf("unsupported database driver: %s", databaseConfig.Driver)
	}
	db, err := gorm.Open(mysql.Open(databaseConfig.Source), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	if err := db.AutoMigrate(&biz.Admin{}, &biz.DriverProfileAuditLog{}); err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	if err := seedAdmin(db, security); err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	cleanup := func() {
		log.Info("closing the data resources")
		_ = sqlDB.Close()
	}
	return &Data{Mdb: db}, cleanup, nil
}

func seedAdmin(db *gorm.DB, security *conf.Security) error {
	if security == nil || security.SeedAdmin == nil || security.SeedAdmin.Username == "" || security.SeedAdmin.Password == "" {
		return nil
	}
	role := security.SeedAdmin.Role
	if role == "" {
		role = biz.RoleSuperAdmin
	}
	var count int64
	if err := db.Model(&biz.Admin{}).Where("username = ?", security.SeedAdmin.Username).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Create(&biz.Admin{
		Username: security.SeedAdmin.Username,
		Password: biz.HashPassword(security.SeedAdmin.Password),
		Role:     role,
		Status:   biz.AdminStatusActive,
	}).Error
}
