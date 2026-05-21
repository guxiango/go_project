package data

import (
	"context"
	"crypto/rand"
	"customer/internal/biz"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type CustomerData struct {
	data *Data
}

// NewCustomerData .
func NewCustomerData(data *Data) *CustomerData {
	return &CustomerData{data: data}
}

// 设置验证码的方法
func (d *CustomerData) SetVerifyCode(ctx context.Context, telephone string, verifyCode string, lifetime int) error {
	status := d.data.Rdb.Set(ctx, "CVC:"+telephone, verifyCode, time.Duration(lifetime)*time.Second)
	if status.Err() != nil {
		return status.Err()
	}
	return nil
}

// 获取手机号对应的验证码
func (d *CustomerData) GetVerifyCode(ctx context.Context, telephone string) (string, error) {
	verifyCode, err := d.data.Rdb.Get(ctx, "CVC:"+telephone).Result()
	if err != nil {
		return "", err
	}
	return verifyCode, nil
}

// 根据手机号获取顾客信息
func (d *CustomerData) GetCustomerByTelephone(ctx context.Context, telephone string) (*biz.Customer, error) {
	// 查询顾客信息
	customer := &biz.Customer{}
	result := d.data.Mdb.Where("telephone = ?", telephone).First(customer)
	// 如果查询成功，则返回顾客信息
	if result.Error == nil && customer.ID > 0 {
		return customer, nil
	}
	if result.Error != nil {
		// 如果查询结果不存在则插入数据
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			customer.Telephone = telephone
			customer.Name = sql.NullString{Valid: false}
			customer.Email = sql.NullString{Valid: false}
			customer.Wechat = sql.NullString{Valid: false}
			result = d.data.Mdb.Create(customer)
			if result.Error != nil {
				return nil, result.Error
			}
			return customer, nil
		}
		// 如果查询结果存在，则返回错误
		return nil, result.Error
	}
	return nil, result.Error
}

// GenerateTokenAndSave 签发 JWT 并落库；返回 token、签发时间 Unix 秒、错误。
func (d *CustomerData) GenerateTokenAndSave(ctx context.Context, customer *biz.Customer, duration time.Duration, secretKey string) (string, int64, error) {
	if customer == nil || customer.ID == 0 {
		return "", 0, errors.New("customer id required for token")
	}
	if secretKey == "" {
		return "", 0, errors.New("jwt secret required")
	}
	jtiRaw := make([]byte, 16)
	if _, err := rand.Read(jtiRaw); err != nil {
		return "", 0, err
	}
	jti := hex.EncodeToString(jtiRaw)
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    "customer",
		Subject:   strconv.FormatUint(uint64(customer.ID), 10),
		Audience:  jwt.ClaimStrings{"customer"},
		ID:        jti,
		ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := tok.SignedString([]byte(secretKey))
	if err != nil {
		return "", 0, err
	}
	customer.Token.Token = tokenString
	customer.Token.TokenCreateAt = sql.NullTime{Time: now, Valid: true}
	if err := d.data.Mdb.WithContext(ctx).Save(customer).Error; err != nil {
		return "", 0, err
	}
	return tokenString, now.Unix(), nil
}

// 根据用户ID获取token
func (d *CustomerData) GetTokenByID(ctx context.Context, customerID string) (string, error) {
	var customer biz.Customer
	if err := d.data.Mdb.WithContext(ctx).Where("id = ?", customerID).First(&customer).Error; err != nil {
		return "", err
	}
	if customer.Token.Token == "" {
		return "", errors.New("token not found")
	}
	return customer.Token.Token, nil
}

// 根据用户ID删除token
func (d *CustomerData) DeleteToken(ctx context.Context, customerID string) error {
	c := &biz.Customer{}
	if err := d.data.Mdb.WithContext(ctx).Where("id = ?", customerID).First(c).Error; err != nil {
		return err
	}
	c.Token.Token = ""
	c.Token.TokenCreateAt = sql.NullTime{Time: time.Now(), Valid: false}
	if err := d.data.Mdb.WithContext(ctx).Save(c).Error; err != nil {
		return err
	}
	return nil
}