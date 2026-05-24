package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	pb "order/api/order"
	"order/internal/auth"
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
	// Page size defaults protect list APIs from accidental large scans.
	defaultPageSize = 20
	maxPageSize     = 100
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

func requireAuth(ctx context.Context) (*auth.User, error) {
	user, ok := auth.FromContext(ctx)
	if !ok || user == nil || user.ID == 0 {
		return nil, kratoserrors.Unauthorized("AUTH_MISSING", "auth user missing")
	}
	user.Role = strings.TrimSpace(strings.ToLower(user.Role))
	if user.Role == "" {
		return nil, kratoserrors.Unauthorized("AUTH_ROLE_MISSING", "auth role missing")
	}
	return user, nil
}

func requireAuthRole(ctx context.Context, role string) (*auth.User, error) {
	user, err := requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if user.Role != role {
		return nil, kratoserrors.Forbidden("FORBIDDEN", role+" role required")
	}
	return user, nil
}

func authorizeOrderAccess(user *auth.User, order *biz.Order) error {
	switch user.Role {
	case "customer":
		if uint64(order.CustomerID) == user.ID {
			return nil
		}
	case "driver":
		if order.DriverID.Valid && uint64(order.DriverID.Int64) == user.ID {
			return nil
		}
	case "admin":
		return nil
	}
	return kratoserrors.Forbidden("ORDER_ACCESS_DENIED", "order access denied")
}

// CreateOrder creates a ride order.
func (s *OrderService) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderReply, error) {
	user, err := requireAuthRole(ctx, "customer")
	if err != nil {
		return nil, err
	}
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
		// OrderNo contains random suffixes; retry only if the unique index collides.
		order = buildOrder(req, user.ID, estimate)
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

func (s *OrderService) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderReply, error) {
	user, err := requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || req.OrderId == 0 {
		return nil, kratoserrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	order, err := s.OrderData.GetOrderById(ctx, int64(req.OrderId))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, kratoserrors.NotFound("ORDER_NOT_FOUND", "order not found")
		}
		s.log.WithContext(ctx).Errorf("get order failed: %v", err)
		return nil, kratoserrors.InternalServer("GET_ORDER_FAILED", "get order failed")
	}
	if err := authorizeOrderAccess(user, order); err != nil {
		return nil, err
	}
	return &pb.GetOrderReply{
		Code:    0,
		Message: "get order success",
		Order:   toOrderInfo(order),
	}, nil
}

func (s *OrderService) ListCustomerOrders(ctx context.Context, req *pb.ListCustomerOrdersRequest) (*pb.ListCustomerOrdersReply, error) {
	user, err := requireAuthRole(ctx, "customer")
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &pb.ListCustomerOrdersRequest{}
	}
	page, pageSize := normalizePage(req.Page, req.PageSize)
	orders, total, err := s.OrderData.ListCustomerOrders(ctx, user.ID, page, pageSize)
	if err != nil {
		s.log.WithContext(ctx).Errorf("list customer orders failed: %v", err)
		return nil, kratoserrors.InternalServer("LIST_CUSTOMER_ORDERS_FAILED", "list customer orders failed")
	}
	return &pb.ListCustomerOrdersReply{
		Code:    0,
		Message: "list customer orders success",
		Orders:  toOrderInfos(orders),
		Total:   total,
	}, nil
}

func (s *OrderService) ListPendingOrders(ctx context.Context, req *pb.ListPendingOrdersRequest) (*pb.ListPendingOrdersReply, error) {
	if _, err := requireAuthRole(ctx, "driver"); err != nil {
		return nil, err
	}
	if req == nil {
		req = &pb.ListPendingOrdersRequest{}
	}
	page, pageSize := normalizePage(req.Page, req.PageSize)
	orders, total, err := s.OrderData.ListPendingOrders(ctx, page, pageSize)
	if err != nil {
		s.log.WithContext(ctx).Errorf("list pending orders failed: %v", err)
		return nil, kratoserrors.InternalServer("LIST_PENDING_ORDERS_FAILED", "list pending orders failed")
	}
	return &pb.ListPendingOrdersReply{
		Code:    0,
		Message: "list pending orders success",
		Orders:  toOrderInfos(orders),
		Total:   total,
	}, nil
}

func (s *OrderService) ListDriverOrders(ctx context.Context, req *pb.ListDriverOrdersRequest) (*pb.ListDriverOrdersReply, error) {
	user, err := requireAuthRole(ctx, "driver")
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &pb.ListDriverOrdersRequest{}
	}
	if err := validateListDriverOrdersRequest(req); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(strings.ToLower(req.Status))
	page, pageSize := normalizePage(req.Page, req.PageSize)
	orders, total, err := s.OrderData.ListDriverOrders(ctx, user.ID, status, page, pageSize)
	if err != nil {
		s.log.WithContext(ctx).Errorf("list driver orders failed: %v", err)
		return nil, kratoserrors.InternalServer("LIST_DRIVER_ORDERS_FAILED", "list driver orders failed")
	}
	return &pb.ListDriverOrdersReply{
		Code:    0,
		Message: "list driver orders success",
		Orders:  toOrderInfos(orders),
		Total:   total,
	}, nil
}

func validateCreateOrderRequest(req *pb.CreateOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_ORDER_REQUEST", "request is required")
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

func validateListDriverOrdersRequest(req *pb.ListDriverOrdersRequest) error {
	if req == nil {
		return nil
	}
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status == "" {
		return nil
	}
	switch biz.OrderStatus(status) {
	case biz.OrderStatusAccepted, biz.OrderStatusStarted, biz.OrderStatusFinished, biz.OrderStatusCancelled:
		return nil
	default:
		return kratoserrors.BadRequest("INVALID_ORDER_STATUS", "status must be accepted, started, finished, or cancelled")
	}
}

func buildOrder(req *pb.CreateOrderRequest, customerID uint64, estimate *biz.EstimateResult) *biz.Order {
	return &biz.Order{
		OrderNo:       GenerateOrderNo(),
		CustomerID:    uint(customerID),
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
	// Nullable columns stay omitted in protobuf replies when the event has not happened.
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

func toOrderInfos(orders []*biz.Order) []*pb.OrderInfo {
	infos := make([]*pb.OrderInfo, 0, len(orders))
	for _, order := range orders {
		infos = append(infos, toOrderInfo(order))
	}
	return infos
}

func normalizePage(page, pageSize int32) (int, int) {
	// Normalize client input once so data-layer queries can assume valid values.
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return int(page), int(pageSize)
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
	user, err := requireAuth(ctx)
	if err != nil {
		return nil, err
	}
	if user.Role != "customer" && user.Role != "driver" {
		return nil, kratoserrors.Forbidden("FORBIDDEN", "customer or driver role required")
	}
	if err := validateCancelOrderRequest(req); err != nil {
		return nil, err
	}

	reason := strings.TrimSpace(req.Reason)
	err = s.OrderData.CancelOrder(ctx, req.OrderId, user.ID, user.Role, reason)
	if err != nil {
		switch kratoserrors.Reason(err) {
		case "INVALID_CANCEL_OPERATOR":
			return nil, kratoserrors.BadRequest("INVALID_CANCEL_OPERATOR", "invalid cancel operator")
		case "ORDER_CANCEL_NOT_ALLOWED":
			return nil, kratoserrors.Conflict("ORDER_CANCEL_NOT_ALLOWED", "only customers can cancel pending or accepted orders, and drivers can cancel accepted orders")
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
	if len(strings.TrimSpace(req.Reason)) > maxCancelReasonLength {
		return kratoserrors.BadRequest("INVALID_CANCEL_REASON", "reason is too long")
	}
	return nil
}

// AcceptOrder The driver accepts the order.
func (s *OrderService) AcceptOrder(ctx context.Context, req *pb.AcceptOrderRequest) (*pb.AcceptOrderReply, error) {
	user, err := requireAuthRole(ctx, "driver")
	if err != nil {
		return nil, err
	}
	if err := validateAcceptOrderRequest(req); err != nil {
		return nil, err
	}
	err = s.OrderData.AcceptOrder(ctx, req.OrderId, user.ID)
	if err != nil {
		switch kratoserrors.Reason(err) {
		case "ORDER_ACCEPT_NOT_ALLOWED":
			return nil, kratoserrors.Conflict("ORDER_ACCEPT_NOT_ALLOWED", "order not found or already accepted")
		default:
			s.log.WithContext(ctx).Errorf("accept order failed: %v", err)
			return nil, kratoserrors.InternalServer("ACCEPT_ORDER_FAILED", "accept order failed")
		}
	}

	order, err := s.OrderData.GetOrderById(ctx, int64(req.OrderId))
	if err != nil {
		s.log.WithContext(ctx).Errorf("get accepted order failed: %v", err)
		return nil, kratoserrors.InternalServer("GET_ACCEPTED_ORDER_FAILED", "get accepted order failed")
	}
	return &pb.AcceptOrderReply{
		Code:    0,
		Message: "accept order success",
		Order:   toOrderInfo(order),
	}, nil

}

func validateAcceptOrderRequest(req *pb.AcceptOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_ACCEPT_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return kratoserrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	return nil
}

// StartOrder The driver starts the accepted order.
func (s *OrderService) StartOrder(ctx context.Context, req *pb.StartOrderRequest) (*pb.StartOrderReply, error) {
	user, err := requireAuthRole(ctx, "driver")
	if err != nil {
		return nil, err
	}
	if err := validateStartOrderRequest(req); err != nil {
		return nil, err
	}
	err = s.OrderData.StartOrder(ctx, req.OrderId, user.ID)
	if err != nil {
		switch kratoserrors.Reason(err) {
		case "ORDER_START_NOT_ALLOWED":
			return nil, kratoserrors.Conflict("ORDER_START_NOT_ALLOWED", "order not found or current status does not allow starting")
		default:
			s.log.WithContext(ctx).Errorf("start order failed: %v", err)
			return nil, kratoserrors.InternalServer("START_ORDER_FAILED", "start order failed")
		}
	}

	order, err := s.OrderData.GetOrderById(ctx, int64(req.OrderId))
	if err != nil {
		s.log.WithContext(ctx).Errorf("get started order failed: %v", err)
		return nil, kratoserrors.InternalServer("GET_STARTED_ORDER_FAILED", "get started order failed")
	}
	return &pb.StartOrderReply{
		Code:    0,
		Message: "start order success",
		Order:   toOrderInfo(order),
	}, nil
}

func validateStartOrderRequest(req *pb.StartOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_START_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return kratoserrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	return nil
}

// FinishOrder The driver finishes the started order.
func (s *OrderService) FinishOrder(ctx context.Context, req *pb.FinishOrderRequest) (*pb.FinishOrderReply, error) {
	user, err := requireAuthRole(ctx, "driver")
	if err != nil {
		return nil, err
	}
	if err := validateFinishOrderRequest(req); err != nil {
		return nil, err
	}
	err = s.OrderData.FinishOrder(ctx, req.OrderId, user.ID)
	if err != nil {
		switch kratoserrors.Reason(err) {
		case "ORDER_FINISH_NOT_ALLOWED":
			return nil, kratoserrors.Conflict("ORDER_FINISH_NOT_ALLOWED", "order not found or current status does not allow finishing")
		default:
			s.log.WithContext(ctx).Errorf("finish order failed: %v", err)
			return nil, kratoserrors.InternalServer("FINISH_ORDER_FAILED", "finish order failed")
		}
	}

	order, err := s.OrderData.GetOrderById(ctx, int64(req.OrderId))
	if err != nil {
		s.log.WithContext(ctx).Errorf("get finished order failed: %v", err)
		return nil, kratoserrors.InternalServer("GET_FINISHED_ORDER_FAILED", "get finished order failed")
	}
	return &pb.FinishOrderReply{
		Code:    0,
		Message: "finish order success",
		Order:   toOrderInfo(order),
	}, nil
}

func validateFinishOrderRequest(req *pb.FinishOrderRequest) error {
	if req == nil {
		return kratoserrors.BadRequest("INVALID_FINISH_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return kratoserrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	return nil
}
