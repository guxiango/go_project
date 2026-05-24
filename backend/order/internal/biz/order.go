package biz

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	mapService "order/api/mapService"
	valuation "order/api/valuation"

	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

const (
	mapServiceName       = "Map"
	valuationServiceName = "Valuation"
)

// OrderStatus is the lifecycle state of an order.
type OrderStatus string

const (
	// Pending orders are waiting for a driver to accept.
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusAccepted  OrderStatus = "accepted"
	OrderStatusStarted   OrderStatus = "started"
	OrderStatusFinished  OrderStatus = "finished"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// Order is the database model for ride orders.
type Order struct {
	gorm.Model
	OrderNo       string        `gorm:"type:varchar(64);uniqueIndex;not null"`
	CustomerID    uint          `gorm:"index;not null"`
	DriverID      sql.NullInt64 `gorm:"index"`
	Origin        string        `gorm:"type:varchar(255);not null"`
	Destination   string        `gorm:"type:varchar(255);not null"`
	Distance      int64         `gorm:"not null;default:0"`
	Duration      int64         `gorm:"not null;default:0"`
	EstimatePrice int64         `gorm:"not null;default:0"`

	Status string `gorm:"type:varchar(32);index;not null"`

	// Cancel fields keep both the actor type and actor id for later audit.
	CancelReason   string        `gorm:"type:varchar(255)"`
	CancelOperator string        `gorm:"type:varchar(32);index"`
	CancelBy       sql.NullInt64 `gorm:"index"`

	AcceptedAt  sql.NullTime
	StartedAt   sql.NullTime
	FinishedAt  sql.NullTime
	CancelledAt sql.NullTime
}

type OrderBiz struct {
	mapClient       mapService.MapServiceClient
	valuationClient valuation.ValuationClient
}

func NewOrderBiz(d *consul.Registry) (*OrderBiz, func(), error) {
	mapConn, err := dial(d, mapServiceName)
	if err != nil {
		return nil, nil, err
	}
	valuationConn, err := dial(d, valuationServiceName)
	if err != nil {
		_ = mapConn.Close()
		return nil, nil, err
	}
	cleanup := func() {
		_ = mapConn.Close()
		_ = valuationConn.Close()
	}
	return &OrderBiz{
		mapClient:       mapService.NewMapServiceClient(mapConn),
		valuationClient: valuation.NewValuationClient(valuationConn),
	}, cleanup, nil
}

type EstimateResult struct {
	Origin      string
	Destination string
	Distance    int64
	Duration    int64
	Price       int64
}

func (ob *OrderBiz) GetEstimatePrice(ctx context.Context, origin, destination string) (*EstimateResult, error) {
	// Map service owns route metadata, while valuation owns the pricing rule.
	drivingInfo, err := ob.mapClient.GetDrivingInfo(ctx, &mapService.GetDrivingInfoRequest{
		Origin:      origin,
		Destination: destination,
	})
	if err != nil {
		return nil, err
	}
	price, err := ob.valuationClient.GetEstimatePrice(ctx, &valuation.GetEstimatePriceRequest{
		Origin:      origin,
		Destination: destination,
	})
	if err != nil {
		return nil, err
	}
	distance, err := strconv.ParseInt(drivingInfo.Distance, 10, 64)
	if err != nil {
		return nil, err
	}
	duration, err := strconv.ParseInt(drivingInfo.Duration, 10, 64)
	if err != nil {
		return nil, err
	}
	return &EstimateResult{
		Origin:      origin,
		Destination: destination,
		Distance:    distance,
		Duration:    duration,
		Price:       price.Price,
	}, nil
}

func dial(d *consul.Registry, serviceName string) (*grpc.ClientConn, error) {
	// All downstream calls are resolved from Consul and carry tracing middleware.
	return kgrpc.DialInsecure(context.Background(),
		kgrpc.WithDiscovery(d),
		kgrpc.WithEndpoint("discovery:///"+serviceName),
		kgrpc.WithMiddleware(tracing.Client()),
		kgrpc.WithTimeout(10*time.Second),
	)
}
