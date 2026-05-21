package biz

import (
	"context"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	mapService "valuation/api/mapService"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

const mapServiceName = "Map"

// 价格规则
type PriceRule struct {
	gorm.Model
	PriceRuleWork
}

type PriceRuleWork struct {
	CityId      uint  `gorm:"column:city_id"`
	StartFee    int64 `gorm:"column:start_fee"`
	DistanceFee int64 `gorm:"column:distance_fee"`
	DurationFee int64 `gorm:"column:duration_fee"`
	StartTime   int   `gorm:"column:start_time;type:int"`
	EndTime     int   `gorm:"column:end_time;type:int"`
}

// 定义操作PriceRule的接口
type PriceRuleInterface interface {
	GetRule(ctx context.Context, cityId uint, curr int) (*PriceRule, error)
}

type ValuationBiz struct {
	priceRuleInterface PriceRuleInterface
	mapClient          mapService.MapServiceClient
}

func NewValuationBiz(priceRuleInterface PriceRuleInterface, d *consul.Registry) (*ValuationBiz, func(), error) {
	conn, err := dial(d, mapServiceName)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = conn.Close() }
	return &ValuationBiz{
		priceRuleInterface: priceRuleInterface,
		mapClient:          mapService.NewMapServiceClient(conn),
	}, cleanup, nil
}

// 获取价格
func (vb *ValuationBiz) GetPrice(ctx context.Context, distance, duration string, cityId uint, curr int) (int64, error) {
	priceRule, err := vb.priceRuleInterface.GetRule(ctx, cityId, curr)
	if err != nil {
		return 0, err
	}
	distanceInt64, _ := strconv.ParseInt(distance, 10, 64)
	durationInt64, _ := strconv.ParseInt(duration, 10, 64)
	distanceInt64 = distanceInt64 / 1000
	durationInt64 = durationInt64 / 60
	var startDistance int64 = 5
	price := priceRule.StartFee + priceRule.DistanceFee*(distanceInt64-startDistance) + priceRule.DurationFee*durationInt64
	return price, nil
}

// 获取驾驶信息(距离、时间)
func (vb *ValuationBiz) GetDrivingInfo(ctx context.Context, origin, destination string) (string, string, error) {
	reply, err := vb.mapClient.GetDrivingInfo(ctx, &mapService.GetDrivingInfoRequest{
		Origin:      origin,
		Destination: destination,
	})
	if err != nil {
		return "", "", err
	}
	return reply.Distance, reply.Duration, nil
}

func dial(d *consul.Registry, serviceName string) (*grpc.ClientConn, error) {
	return kgrpc.DialInsecure(context.Background(),
		kgrpc.WithDiscovery(d),
		kgrpc.WithEndpoint("discovery:///"+serviceName),
		kgrpc.WithMiddleware(tracing.Client()),
		kgrpc.WithTimeout(10*time.Second),
	)
}
