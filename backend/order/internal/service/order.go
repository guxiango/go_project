package service

import (
	"context"
	"fmt"
	"math/rand"
	pb "order/api/order"
	"order/internal/biz"
	"order/internal/data"
	"time"
)

type OrderService struct {
	pb.UnimplementedOrderServer
	OrderBiz  *biz.OrderBiz
	OrderData *data.OrderData
}

func NewOrderService(OrderBiz *biz.OrderBiz, OrderData *data.OrderData) *OrderService {
	return &OrderService{
		OrderBiz:  OrderBiz,
		OrderData: OrderData,
	}
}

// CreateOrder 创建订单
func (s *OrderService) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderReply, error) {
	orderNo := GenerateOrderNo()
	order := &biz.Order{
		OrderNo:       orderNo,
		CustomerID:    uint(req.CustomerId),
		Origin:        req.Origin,
		Destination:   req.Destination,
		Distance:      req.Distance,
		Duration:      req.Duration,
		EstimatePrice: req.EstimatePrice,
		Status:        string(biz.OrderStatusPending),
	}
	err := s.OrderData.CreateOrderData(ctx, order)
	if err != nil {
		return &pb.CreateOrderReply{
			Code:    1,
			Message: "订单创建失败，请稍后重试",
		}, nil
	}
	return &pb.CreateOrderReply{
		Code:    0,
		Message: "创建订单成功",
		Order: &pb.OrderInfo{
			CustomerId:    uint64(order.CustomerID),
			Origin:        order.Origin,
			Destination:   order.Destination,
			Distance:      order.Distance,
			Duration:      order.Duration,
			EstimatePrice: order.EstimatePrice,
		},
	}, nil
}

// GenerateOrderNo 生成订单编号
func GenerateOrderNo() string {
	return fmt.Sprintf(
		"OD%s%06d",
		time.Now().Format("20060102150405000"),
		rand.Intn(1000000),
	)
}
