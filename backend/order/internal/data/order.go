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

var customerCancelableStatuses = []string{
	string(biz.OrderStatusPending),
	string(biz.OrderStatusAccepted),
}

func NewOrderData(data *Data, OrderBiz *biz.OrderBiz) *OrderData {
	return &OrderData{
		data:     data,
		OrderBiz: OrderBiz,
	}
}

// GetOrderById queries an order by its primary id.
func (order *OrderData) GetOrderById(ctx context.Context, id int64) (*biz.Order, error) {
	ord := &biz.Order{}
	result := order.data.Mdb.WithContext(ctx).Where("id=?", id).First(ord)
	if result.Error != nil {
		return nil, result.Error
	}
	return ord, nil
}

func (order *OrderData) ListCustomerOrders(ctx context.Context, customerID uint64, page, pageSize int) ([]*biz.Order, int64, error) {
	var orders []*biz.Order
	var total int64
	db := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("customer_id = ?", customerID)
	// Count is executed before pagination so the API can return the full result size.
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (order *OrderData) ListPendingOrders(ctx context.Context, page, pageSize int) ([]*biz.Order, int64, error) {
	var orders []*biz.Order
	var total int64
	db := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("status = ?", string(biz.OrderStatusPending))
	// Drivers need a paged list, but clients still need total to render pagination.
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (order *OrderData) ListDriverOrders(ctx context.Context, driverID uint64, status string, page, pageSize int) ([]*biz.Order, int64, error) {
	var orders []*biz.Order
	var total int64
	db := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("driver_id = ?", driverID)
	if status != "" {
		db = db.Where("status = ?", status)
	}
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
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
		// Customers may cancel only pending or accepted orders.
		result = order.data.Mdb.WithContext(ctx).
			Model(&biz.Order{}).
			Where("id = ? AND customer_id = ? AND status IN ?", orderId, operatorId, customerCancelableStatuses).
			Updates(updates)
	case "driver":
		// Drivers may cancel only their own accepted orders.
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
		// Zero rows means either the id/operator did not match or the state changed concurrently.
		return errors.New(409, "ORDER_CANCEL_NOT_ALLOWED", "order not found, operator mismatch, or status does not allow cancellation")
	}
	return nil
}

// AcceptOrder The driver accepts the order and modifies the order status.
func (order *OrderData) AcceptOrder(ctx context.Context, orderId uint64, driverId uint64) error {
	result := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("id = ? AND status = ?", orderId, string(biz.OrderStatusPending)).Updates(map[string]interface{}{
		"driver_id":   driverId,
		"status":      string(biz.OrderStatusAccepted),
		"accepted_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(409, "ORDER_ACCEPT_NOT_ALLOWED", "order not found or already accepted")
	}
	return nil
}

// StartOrder starts an accepted order for the assigned driver.
func (order *OrderData) StartOrder(ctx context.Context, orderId uint64, driverId uint64) error {
	result := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("id = ? AND driver_id = ? AND status = ?", orderId, driverId, string(biz.OrderStatusAccepted)).Updates(map[string]interface{}{
		"status":     string(biz.OrderStatusStarted),
		"started_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(409, "ORDER_START_NOT_ALLOWED", "order not found or current status does not allow starting")
	}
	return nil
}

// FinishOrder finishes a started order for the assigned driver.
func (order *OrderData) FinishOrder(ctx context.Context, orderId uint64, driverId uint64) error {
	result := order.data.Mdb.WithContext(ctx).Model(&biz.Order{}).Where("id = ? AND driver_id = ? AND status = ?", orderId, driverId, string(biz.OrderStatusStarted)).Updates(map[string]interface{}{
		"status":      string(biz.OrderStatusFinished),
		"finished_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New(409, "ORDER_FINISH_NOT_ALLOWED", "order not found or current status does not allow finishing")
	}
	return nil
}
