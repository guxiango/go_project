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

	jwtv5 "github.com/golang-jwt/jwt/v5"
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

const (
	DriverStatusStop     = "stop"     // 停止服务
	DriverStatusPending  = "pending"  // 待审核
	DriverStatusApproved = "approved" // 审核通过
	DriverStatusOnline   = "online"   // 在线
	DriverStatusBusy     = "busy"     // 忙碌
	DriverStatusOffline  = "offline"  // 离线
	DriverStatusBlocked  = "blocked"  // 封禁
)

// 业务模型
type DriverWork struct {
	Telephone     string         `gorm:"type:varchar(16);uniqueIndex;" json:"telephone"`
	Token         sql.NullString `gorm:"type:varchar(512);" json:"token"`
	Name          sql.NullString `gorm:"type:varchar(255);index;" json:"name"`
	Status        sql.NullString `gorm:"type:enum('stop','pending','approved','online','busy','offline','blocked');" json:"status"`
	TokenCreateAt sql.NullTime   `gorm:"type:datetime;" json:"token_create_at"`
	IdNumber      sql.NullString `gorm:"type:varchar(18);uniqueIndex;" json:"id_number"`
	IdImageA      sql.NullString `gorm:"type:varchar(255);" json:"id_image_a"`
	IdImageB      sql.NullString `gorm:"type:varchar(255);" json:"id_image_b"`
	LicenseImageA sql.NullString `gorm:"type:varchar(255);" json:"license_image_a"`
	LicenseImageB sql.NullString `gorm:"type:varchar(255);" json:"license_image_b"`
	DistinctCode  sql.NullString `gorm:"type:varchar(255);index" json:"distinct_code"`
	Auditat       sql.NullTime   `gorm:"type:datetime;" json:"auditat"`
	TelephoneBak  sql.NullString `gorm:"type:varchar(255);index" json:"telephone_bak"`
}

// 定义DriverClaims结构体，用于JWT的Claims部分'
type DriverClaims struct {
	DriverId   uint   `json:"driver_id"`
	DriverName string `json:"driver_name"`
	jwtv5.RegisteredClaims
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

func (db *DriverBiz) InitDriverInfo(ctx context.Context, tel string) (*Driver, error) {

	return nil, nil
}
