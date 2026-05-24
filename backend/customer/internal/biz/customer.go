package biz

import (
	"context"
	"database/sql"
	"gorm.io/gorm"
	"time"

	order "customer/api/order"
	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	verifyCode "verify-code/api/verifyCode"
)

// 模型
type Customer struct {
	// 嵌入业务逻辑部分
	CustomerWork
	// 嵌入token部分
	Token
	// 嵌入基础字段
	gorm.Model
}

// 业务逻辑部分
type CustomerWork struct {
	Telephone string         `gorm:"type:varchar(11);uniqueIndex;" json:"telephone"`
	Name      sql.NullString `gorm:"type:varchar(255);uniqueIndex;" json:"name"`
	Email     sql.NullString `gorm:"type:varchar(255);uniqueIndex;" json:"email"`
	Wechat    sql.NullString `gorm:"type:varchar(255);uniqueIndex;" json:"wechat"`
}

// token 部分
type Token struct {
	// JWT（HS256）常见 200～400+ 字节，255 易触发 MySQL 1406；用 512 或 text 更稳妥
	Token         string       `gorm:"type:varchar(512);column:token" json:"token"`
	TokenCreateAt sql.NullTime `gorm:"type:datetime;" json:"token_create_at"`
}

const (
	orderServiceName      = "Order"
	verifyCodeServiceName = "VerifyCode"
)

type CustomerBiz struct {
	order      order.OrderClient
	verifyCode verifyCode.VerifyCodeClient
}

func NewCustomerBiz(d *consul.Registry) (*CustomerBiz, func(), error) {
	orderConn, err := dial(d, orderServiceName)
	if err != nil {
		return nil, nil, err
	}
	verifyCodeConn, err := dial(d, verifyCodeServiceName)
	if err != nil {
		_ = orderConn.Close()
		return nil, nil, err
	}
	cleanup := func() {
		orderConn.Close()
		verifyCodeConn.Close()
	}
	orderClient := order.NewOrderClient(orderConn)
	verifyCodeClient := verifyCode.NewVerifyCodeClient(verifyCodeConn)
	return &CustomerBiz{orderClient, verifyCodeClient}, cleanup, nil
}

func (cb *CustomerBiz) GetEstimatePrice(ctx context.Context, origin, destination string) (int64, error) {
	reply, err := cb.order.GetEstimatePrice(ctx, &order.GetEstimatePriceRequest{
		Origin:      origin,
		Destination: destination,
	})
	if err != nil {
		return 0, err
	}
	return reply.Price, nil
}

func (cb *CustomerBiz) GetVerifyCode(ctx context.Context, length uint32, typ verifyCode.TYPE) (*verifyCode.GetVerifyCodeReply, error) {
	reply, err := cb.verifyCode.GetVerifyCode(ctx, &verifyCode.GetVerifyCodeRequest{
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
