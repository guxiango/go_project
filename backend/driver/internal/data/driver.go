package data

import (
	"context"
	"time"
)

type DriverData struct {
	data *Data
}

func NewDriverData(data *Data) *DriverData {
	return &DriverData{data: data}
}

// 向redis中存储生成的验证码
func (d *DriverData) SetVerifyCode(ctx context.Context, telephone string, verifyCode string, lifetime int) error {
	status := d.data.Rdb.Set(ctx, "DVC:"+telephone, verifyCode, time.Duration(lifetime)*time.Second)
	if status.Err() != nil {
		return status.Err()
	}
	return nil
}