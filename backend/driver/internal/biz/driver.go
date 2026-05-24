package biz

import (
	"context"
	"database/sql"
	orderAPI "driver/api/order"
	verifyCode "driver/api/verifyCode"
	"errors"
	"time"

	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

const (
	verifyCodeServiceName = "VerifyCode"
	orderServiceName      = "Order"
)

type DriverBiz struct {
	verifyCode verifyCode.VerifyCodeClient
	order      orderAPI.OrderClient
}

// Driver is the database model for driver accounts.
type Driver struct {
	gorm.Model
	DriverWork
}

const (
	DriverStatusStop     = "stop"     // Stopped.
	DriverStatusPending  = "pending"  // Waiting for review.
	DriverStatusApproved = "approved" // Review approved.
	DriverStatusOnline   = "online"   // Online and available.
	DriverStatusBusy     = "busy"     // Busy with work.
	DriverStatusOffline  = "offline"  // Offline.
	DriverStatusBlocked  = "blocked"  // Blocked.
)

// DriverWork contains editable driver profile and work fields.
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

// DriverClaims stores driver identity in JWT claims.
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
	orderConn, err := dial(d, orderServiceName)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		verifyCodeConn.Close()
		orderConn.Close()
	}
	verifyCodeClient := verifyCode.NewVerifyCodeClient(verifyCodeConn)
	orderClient := orderAPI.NewOrderClient(orderConn)
	return &DriverBiz{
		verifyCode: verifyCodeClient,
		order:      orderClient,
	}, cleanup, nil
}

// GetVerifyCode fetches a verification code from the verify-code service.
func (db *DriverBiz) GetVerifyCode(ctx context.Context, telephone string, length uint32, typ verifyCode.TYPE) (*verifyCode.GetVerifyCodeReply, error) {
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
func (db *DriverBiz) OrderClient() (orderAPI.OrderClient, error) {
	if db.order != nil {
		return db.order, nil
	}
	return nil, errors.New("The client has not yet been created.")
}
