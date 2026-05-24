package data

import (
	"context"
	"order/internal/biz"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
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

// GetOrderById 根据订单id查询订单
func (order *OrderData) GetOrderById(ctx context.Context, id int64) (*biz.Order, error) {
	ord := &biz.Order{}
	result := order.data.Mdb.WithContext(ctx).Where("id=?", id).First(ord)
	if result.Error != nil {
		return nil, result.Error
	}
	return ord, nil
}

// CreateOrderData creates an order.
func (order *OrderData) CreateOrderData(ctx context.Context, orders *biz.Order) error {
	result := order.data.Mdb.WithContext(ctx).Create(orders)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// CancelOrder cancels an order with a conditional update to protect state transitions.
func (order *OrderData) CancelOrder(ctx context.Context, orderId uint64, operatorId uint64, operatorType string, reason string) error {
	updates := map[string]interface{}{
		"status":          string(biz.OrderStatusCancelled),
		"cancelled_at":    time.Now(),
		"cancel_reason":   reason,
		"cancel_operator": operatorType,
		"cancel_by":       operatorId,
	}

	var result *gorm.DB
	switch operatorType {
	case "customer":
		result = order.data.Mdb.WithContext(ctx).
			Model(&biz.Order{}).
			Where("id = ? AND customer_id = ? AND status IN ?", orderId, operatorId, []string{
				string(biz.OrderStatusPending),
				string(biz.OrderStatusAccepted),
			}).
			Updates(updates)
	case "driver":
		result = order.data.Mdb.WithContext(ctx).
			Model(&biz.Order{}).
			Where("id = ? AND driver_id = ? AND status = ?", orderId, operatorId, string(biz.OrderStatusAccepted)).
			Updates(updates)
	default:
		return errors.New(400, "INVALID_CANCEL_OPERATOR", "invalid cancel operator")
	}

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(409, "ORDER_CANCEL_NOT_ALLOWED", "order not found or current status does not allow cancellation")
	}
	return nil
}
