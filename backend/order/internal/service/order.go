package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	pb "order/api/order"
	"order/internal/biz"
	"order/internal/data"
	"strings"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

const (
	maxCreateOrderRetries = 3
	maxCancelReasonLength = 255
)

type OrderService struct {
	pb.UnimplementedOrderServer
	OrderBiz  *biz.OrderBiz
	OrderData *data.OrderData
	log       *log.Helper
}

func NewOrderService(OrderBiz *biz.OrderBiz, OrderData *data.OrderData, logger log.Logger) *OrderService {
	return &OrderService{
		OrderBiz:  OrderBiz,
		OrderData: OrderData,
		log:       log.NewHelper(logger),
	}
}

// CreateOrder creates a ride order.
func (s *OrderService) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderReply, error) {
	if err := validateCreateOrderRequest(req); err != nil {
		return nil, err
	}
	estimate, err := s.OrderBiz.GetEstimatePrice(ctx, strings.TrimSpace(req.Origin), strings.TrimSpace(req.Destination))
	if err != nil {
		s.log.WithContext(ctx).Errorf("estimate order price failed: %v", err)
		return nil, kratoserrors.InternalServer("ESTIMATE_ORDER_PRICE_FAILED", "estimate order price failed")
	}

	var order *biz.Order
	for i := 0; i < maxCreateOrderRetries; i++ {
		order = buildOrder(req, estimate)
		err = s.OrderData.CreateOrderData(ctx, order)
		if err == nil {
			return &pb.CreateOrderReply{
				Code:    0,
				Message: "create order success",
				Order:   toOrderInfo(order),
			}, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			s.log.WithContext(ctx).Errorf("create order failed: %v", err)
			return nil, kratoserrors.InternalServer("CREATE_ORDER_FAILED", "create order failed")
		}
	}

	s.log.WithContext(ctx).Errorf("create order failed after retrying order no: %v", err)
	return nil, kratoserrors.InternalServer("ORDER_NO_DUPLICATED", "create order failed")
}

// GetEstimatePrice estimates distance, duration, and price for an order.
func (s *OrderService) GetEstimatePrice(ctx context.Context, req *pb.GetEstimatePriceRequest) (*pb.GetEstimatePriceReply, error) {
	if err := validateEstimatePriceRequest(req.GetOrigin(), req.GetDestination()); err != nil {
		return nil, err
	}
	origin := strings.TrimSpace(req.GetOrigin())
	destination := strings.TrimSpace(req.GetDestination())
	estimate, err := s.OrderBiz.GetEstimatePrice(ctx, origin, destination)
	if err != nil {
		s.log.WithContext(ctx).Errorf("estimate order price failed: %v", err)
		return nil, kratoserrors.InternalServer("ESTIMATE_ORDER_PRICE_FAILED", "estimate order price failed")
	}
	return &pb.GetEstimatePriceReply{
		Code:        0,
		Message:     "estimate price success",
		Origin:      estimate.Origin,
		Destination: estimate.Destination,
		Distance:    estimate.Distance,
		Duration:    estimate.Duration,
		Price:       estimate.Price,
	}, nil
}

func validateCreateOrderRequest(req *pb.CreateOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_ORDER_REQUEST", "request is required")
	}
	if req.CustomerId == 0 {
		return kratoserrors.BadRequest("INVALID_CUSTOMER_ID", "customer_id is required")
	}
	return validateEstimatePriceRequest(req.Origin, req.Destination)
}

func validateEstimatePriceRequest(origin, destination string) error {
	if strings.TrimSpace(origin) == "" {
		return kratoserrors.BadRequest("INVALID_ORIGIN", "origin is required")
	}
	if strings.TrimSpace(destination) == "" {
		return kratoserrors.BadRequest("INVALID_DESTINATION", "destination is required")
	}
	return nil
}

func buildOrder(req *pb.CreateOrderRequest, estimate *biz.EstimateResult) *biz.Order {
	return &biz.Order{
		OrderNo:       GenerateOrderNo(),
		CustomerID:    uint(req.CustomerId),
		Origin:        strings.TrimSpace(req.Origin),
		Destination:   strings.TrimSpace(req.Destination),
		Distance:      estimate.Distance,
		Duration:      estimate.Duration,
		EstimatePrice: estimate.Price,
		Status:        string(biz.OrderStatusPending),
	}
}

func toOrderInfo(order *biz.Order) *pb.OrderInfo {
	info := &pb.OrderInfo{
		OrderId:       uint64(order.ID),
		OrderNo:       order.OrderNo,
		CustomerId:    uint64(order.CustomerID),
		Origin:        order.Origin,
		Destination:   order.Destination,
		Distance:      order.Distance,
		Duration:      order.Duration,
		EstimatePrice: order.EstimatePrice,
		Status:        order.Status,
		CreatedAt:     order.CreatedAt.Unix(),
	}
	if order.DriverID.Valid && order.DriverID.Int64 > 0 {
		info.DriverId = uint64(order.DriverID.Int64)
	}
	if order.AcceptedAt.Valid {
		info.AcceptedAt = order.AcceptedAt.Time.Unix()
	}
	if order.StartedAt.Valid {
		info.StartedAt = order.StartedAt.Time.Unix()
	}
	if order.FinishedAt.Valid {
		info.FinishedAt = order.FinishedAt.Time.Unix()
	}
	if order.CancelledAt.Valid {
		info.CancelledAt = order.CancelledAt.Time.Unix()
	}
	return info
}

// GenerateOrderNo generates a public order number.
func GenerateOrderNo() string {
	return fmt.Sprintf(
		"OD%s%06d",
		time.Now().Format("20060102150405000"),
		rand.Intn(1000000),
	)
}

// CancelOrder cancels an order.
func (s *OrderService) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderReply, error) {
	if err := validateCancelOrderRequest(req); err != nil {
		return nil, err
	}

	operatorType := strings.TrimSpace(strings.ToLower(req.OperatorType))
	reason := strings.TrimSpace(req.Reason)
	err := s.OrderData.CancelOrder(ctx, req.OrderId, req.OperatorId, operatorType, reason)
	if err != nil {
		switch kratoserrors.Reason(err) {
		case "INVALID_CANCEL_OPERATOR":
			return nil, kratoserrors.BadRequest("INVALID_CANCEL_OPERATOR", "invalid cancel operator")
		case "ORDER_CANCEL_NOT_ALLOWED":
			return nil, kratoserrors.Conflict("ORDER_CANCEL_NOT_ALLOWED", "order not found or current status does not allow cancellation")
		default:
			s.log.WithContext(ctx).Errorf("cancel order failed: %v", err)
			return nil, kratoserrors.InternalServer("CANCEL_ORDER_FAILED", "cancel order failed")
		}
	}

	order, err := s.OrderData.GetOrderById(ctx, int64(req.OrderId))
	if err != nil {
		s.log.WithContext(ctx).Errorf("get cancelled order failed: %v", err)
		return nil, kratoserrors.InternalServer("GET_CANCELLED_ORDER_FAILED", "get cancelled order failed")
	}
	return &pb.CancelOrderReply{
		Code:    0,
		Message: "cancel order success",
		Order:   toOrderInfo(order),
	}, nil
}

func validateCancelOrderRequest(req *pb.CancelOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_CANCEL_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return kratoserrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	operatorType := strings.TrimSpace(strings.ToLower(req.OperatorType))
	if operatorType != "customer" && operatorType != "driver" {
		return kratoserrors.BadRequest("INVALID_CANCEL_OPERATOR", "operator_type must be customer or driver")
	}
	if req.OperatorId == 0 {
		return kratoserrors.BadRequest("INVALID_OPERATOR_ID", "operator_id is required")
	}
	if len(strings.TrimSpace(req.Reason)) > maxCancelReasonLength {
		return kratoserrors.BadRequest("INVALID_CANCEL_REASON", "reason is too long")
	}
	return nil
}
