package data

import (
	"context"
	"order/internal/biz"
)

type OrderData struct {
	data     *Data
	OrderBiz *biz.OrderBiz
}

func NewOrderData(data *Data, OrderBiz *biz.OrderBiz) *OrderData {
	return &OrderData{
		data:     data,
		OrderBiz: OrderBiz,
	}
}

// 创建订单
func (order *OrderData) CreateOrderData(ctx context.Context, orders *biz.Order) error {
	result := order.data.Mdb.WithContext(ctx).Create(orders)
	if result.Error != nil {
		return result.Error
	}
	return nil
}
