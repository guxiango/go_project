package data

import (
	"context"
	"database/sql"
	"driver/internal/biz"
	"errors"
	"time"

	"gorm.io/gorm"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

type DriverData struct {
	data *Data
}

func NewDriverData(data *Data) *DriverData {
	return &DriverData{data: data}
}

// SetVerifyCode 向redis中存储生成的验证码
func (d *DriverData) SetVerifyCode(ctx context.Context, telephone string, verifyCode string, lifetime int) error {
	status := d.data.Rdb.Set(ctx, "DVC:"+telephone, verifyCode, time.Duration(lifetime)*time.Second)
	if status.Err() != nil {
		return status.Err()
	}
	return nil
}

// GetVerifyCode 从redis中获取验证码
func (d *DriverData) GetVerifyCode(ctx context.Context, telephone string) (string, error) {
	VerifyCode, err := d.data.Rdb.Get(ctx, "DVC:"+telephone).Result()
	if err != nil {
		return "", err
	}
	return VerifyCode, nil
}

// GetDriverByPhone 根据手机号查询司机信息
func (d *DriverData) GetDriverByPhone(ctx context.Context, telephone string) (*biz.Driver, error) {
	var driver biz.Driver
	result := d.data.Mdb.Where("telephone = ?", telephone).First(&driver)
	if result.Error != nil {
		// 如果没有找到记录，返回nil和gorm.ErrRecordNotFount错误
		if result.Error == gorm.ErrRecordNotFound {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return &driver, result.Error
}

// CreateDriver 创建司机记录
func (d *DriverData) CreateDriver(ctx context.Context, telephone string) (*biz.Driver, error) {
	driver := &biz.Driver{
		DriverWork: biz.DriverWork{
			Telephone: telephone,
			Status: sql.NullString{
				String: biz.DriverStatusStop,
				Valid:  true,
			},
		},
	}
	result := d.data.Mdb.WithContext(ctx).Create(driver)
	return driver, result.Error
}

// GenerateTokenAndSave 签发JWT令牌并保存到mysql数据库中
func (d *DriverData) GenerateTokenAndSave(ctx context.Context, driver *biz.Driver, duration time.Duration, secretKey string) (string,
	error) {
	if driver == nil || driver.ID == 0 {
		return "", errors.New("invalid driver")
	}
	jwtSecret := []byte(secretKey)
	claims := &biz.DriverClaims{
		DriverId:   driver.ID,
		DriverName: driver.Name.String,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    "laomadj_driver",
			Subject:   "DriverToken",
			ExpiresAt: jwtv5.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwtv5.NewNumericDate(time.Now()),
		},
	}
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", err
	}
	// 将token保存到mysql数据库中
	driver.Token = sql.NullString{
		String: tokenString,
		Valid:  true,
	}
	driver.TokenCreateAt = sql.NullTime{
		Time:  time.Now(),
		Valid: true,
	}
	result := d.data.Mdb.WithContext(ctx).Save(driver)
	if result.Error != nil {
		return "", result.Error
	}
	return tokenString, nil
}

// GetTokenByID 根据id获取token
func (d *DriverData) GetTokenByID(ctx context.Context, id uint) (string, error) {
	var driver biz.Driver
	if err := d.data.Mdb.WithContext(ctx).First(&driver, id).Error; err != nil {
		return "", err
	}
	if !driver.Token.Valid || driver.Token.String == "" {
		return "", errors.New("invalid token")
	}
	return driver.Token.String, nil
}

// UpdateDriverProfileByID 根据id更新司机资料
func (d *DriverData) UpdateDriverProfileByID(ctx context.Context, id uint, profile biz.DriverWork) error {
	updates := map[string]interface{}{}
	if profile.Status.Valid {
		updates["status"] = profile.Status
	}
	if profile.Name.Valid {
		updates["name"] = profile.Name
	}
	if profile.IdNumber.Valid {
		updates["id_number"] = profile.IdNumber
	}
	if profile.IdImageA.Valid {
		updates["id_image_a"] = profile.IdImageA
	}
	if profile.IdImageB.Valid {
		updates["id_image_b"] = profile.IdImageB
	}
	if profile.LicenseImageA.Valid {
		updates["license_image_a"] = profile.LicenseImageA
	}
	if profile.LicenseImageB.Valid {
		updates["license_image_b"] = profile.LicenseImageB
	}
	if profile.DistinctCode.Valid {
		updates["distinct_code"] = profile.DistinctCode
	}
	if profile.TelephoneBak.Valid {
		updates["telephone_bak"] = profile.TelephoneBak
	}

	result := d.data.Mdb.WithContext(ctx).Model(&biz.Driver{}).Where("id = ?", id).Updates(updates)
	return result.Error
}

// 根据id获取司机信息
func (d *DriverData) GetDriverByID(ctx context.Context, id uint) (*biz.Driver, error) {
	var driver biz.Driver
	if err := d.data.Mdb.WithContext(ctx).First(&driver, id).Error; err != nil {
		return nil, err
	}
	return &driver, nil
}

// 根据id修改司机的状态
func (d *DriverData) UpdateDriverStatusByID(ctx context.Context, id uint, currentStatus string, targetStatus string) error {
	result := d.data.Mdb.WithContext(ctx).
		Model(&biz.Driver{}).
		Where("id = ? AND status = ?", id, currentStatus).
		Update("status", targetStatus)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("driver status changed")
	}
	return nil
}

func (d *DriverData) UpdateDriverStatusFromAny(ctx context.Context, id uint, currentStatuses []string, targetStatus string) (string, error) {
	if len(currentStatuses) == 0 {
		return "", errors.New("driver current status is required")
	}
	returnedStatus := ""
	err := d.data.Mdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var driver biz.Driver
		if err := tx.First(&driver, id).Error; err != nil {
			return err
		}
		if !driver.Status.Valid || driver.Status.String == "" {
			return errors.New("driver status missing")
		}
		allowed := false
		for _, status := range currentStatuses {
			if driver.Status.String == status {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("driver status changed")
		}
		result := tx.Model(&biz.Driver{}).
			Where("id = ? AND status = ?", id, driver.Status.String).
			Update("status", targetStatus)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("driver status changed")
		}
		returnedStatus = driver.Status.String
		return nil
	})
	if err != nil {
		return "", err
	}
	return returnedStatus, nil
}

// AuditDriverStatusByID 根据id审核司机资料并修改状态
func (d *DriverData) AuditDriverStatusByID(ctx context.Context, id uint, currentStatus string, targetStatus string) error {
	result := d.data.Mdb.WithContext(ctx).
		Model(&biz.Driver{}).
		Where("id = ? AND status = ?", id, currentStatus).
		Updates(map[string]interface{}{
			"status":  sql.NullString{String: targetStatus, Valid: true},
			"auditat": sql.NullTime{Time: time.Now(), Valid: true},
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("driver status changed")
	}
	return nil
}

// 根据状态分页查询司机信息
func (d *DriverData) ListDriversByStatus(ctx context.Context, status string, offset int, limit int) ([]*biz.Driver, int64, error) {
	var drivers []*biz.Driver
	var total int64
	err := d.data.Mdb.WithContext(ctx).
		Model(&biz.Driver{}).
		Where("status = ?", status).
		Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = d.data.Mdb.WithContext(ctx).
		Model(&biz.Driver{}).
		Where("status = ?", status).
		Order("updated_at desc").
		Offset(offset).
		Limit(limit).
		Find(&drivers).Error
	if err != nil {
		return nil, 0, err
	}
	return drivers, total, nil
}
