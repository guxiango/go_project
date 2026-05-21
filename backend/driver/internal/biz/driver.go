package biz

import (
	"context"
	"database/sql"
	verifyCode "driver/api/verifyCode"
	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	"gorm.io/gorm"
	"time"
)

const (
	verifyCodeServiceName = "VerifyCode"
)

type DriverBiz struct {
	verifyCode verifyCode.VerifyCodeClient
}

// 定义司机表的模型
type Driver struct {
	// 基础模型
	gorm.Model
	// 业务模型
	DriverWork
}

// 业务模型
type DriverWork struct {
	Telephone     string         `gorm:"type:varchar(16);uniqueIndex;" json:"telephone"`
	Token         sql.NullString `gorm:"type:varchar(512);" json:"token"`
	Name          sql.NullString `gorm:"type:varchar(255);index;" json:"name"`
	Status        sql.NullString `gorm:"type:enum('out', 'in', 'listen', 'stop');" json:"status"`
	IdNumber      sql.NullString `gorm:"type:varchar(18);uniqueIndex;" json:"id_number"`
	IdImageA      sql.NullString `gorm:"type:varchar(255);" json:"id_image_a"`
	IdImageB      sql.NullString `gorm:"type:varchar(255);" json:"id_image_b"`
	LicenseImageA sql.NullString `gorm:"type:varchar(255);" json:"license_image_a"`
	LicenseImageB sql.NullString `gorm:"type:varchar(255);" json:"license_image_b"`
	DistinctCode  sql.NullString `gorm:"type:varchar(255);index" json:"distinct_code"`
	Auditat       sql.NullTime   `gorm:"type:datetime;" json:"auditat"`
	TelephoneBak  sql.NullString `gorm:"type:varchar(255);index" json:"telephone_bak"`
}

func NewDriverBiz(d *consul.Registry) (*DriverBiz, func(), error) {
	verifyCodeConn, err := dial(d, verifyCodeServiceName)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		verifyCodeConn.Close()
	}
	verifyCodeClient := verifyCode.NewVerifyCodeClient(verifyCodeConn)
	return &DriverBiz{verifyCode: verifyCodeClient}, cleanup, nil
}

// 获取验证码
func (db *DriverBiz) GetVerifyCode(ctx context.Context, telephone string, length uint32, typ verifyCode.TYPE) (*verifyCode.GetVerifyCodeReply, error) {
	// 调用验证码服务来获取验证码
	reply, err := db.verifyCode.GetVerifyCode(ctx, &verifyCode.GetVerifyCodeRequest{
		Length: length,
		Type:   typ,
	})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

func dial(d *consul.Registry, serviceName string) (*grpc.ClientConn, error) {
	conn, err := kgrpc.DialInsecure(context.Background(),
		kgrpc.WithDiscovery(d),
		kgrpc.WithEndpoint("discovery:///"+serviceName),
		kgrpc.WithMiddleware(tracing.Client()),
		kgrpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
