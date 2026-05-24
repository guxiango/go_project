package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"admin/internal/biz"

	"gorm.io/gorm"
)

type adminRepo struct {
	data *Data
}

func NewAdminRepo(data *Data) biz.AdminRepo {
	return &adminRepo{data: data}
}

func (r *adminRepo) FindByUsername(ctx context.Context, username string) (*biz.Admin, error) {
	var admin biz.Admin
	if err := r.data.Mdb.WithContext(ctx).Where("username = ?", username).First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}
	return &admin, nil
}

func (r *adminRepo) FindByID(ctx context.Context, id uint) (*biz.Admin, error) {
	var admin biz.Admin
	if err := r.data.Mdb.WithContext(ctx).First(&admin, id).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}

func (r *adminRepo) SaveToken(ctx context.Context, admin *biz.Admin, token string, now time.Time) error {
	return r.data.Mdb.WithContext(ctx).
		Model(&biz.Admin{}).
		Where("id = ?", admin.ID).
		Updates(map[string]interface{}{
			"token":           sql.NullString{String: token, Valid: true},
			"token_create_at": sql.NullTime{Time: now, Valid: true},
		}).Error
}

func (r *adminRepo) CreateAuditLog(ctx context.Context, log *biz.DriverProfileAuditLog) error {
	return r.data.Mdb.WithContext(ctx).Create(log).Error
}
