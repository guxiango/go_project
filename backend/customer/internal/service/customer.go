package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	pb "customer/api/customer"
	orderAPI "customer/api/order"
	customerBiz "customer/internal/biz"
	"customer/internal/conf"
	customerData "customer/internal/data"
	verifyCode "verify-code/api/verifyCode"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	jwt "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/metadata"
)

type CustomerService struct {
	pb.UnimplementedCustomerServer
	CustomerData *customerData.CustomerData
	security     *conf.Security
	CustomerBiz  *customerBiz.CustomerBiz
}

func NewCustomerService(customerData *customerData.CustomerData, security *conf.Security, customerBiz *customerBiz.CustomerBiz) *CustomerService {
	return &CustomerService{
		CustomerData: customerData,
		security:     security,
		CustomerBiz:  customerBiz,
	}
}

func (s *CustomerService) GetVerifyCode(ctx context.Context, req *pb.GetVerifyCodeReq) (*pb.GetVerifyCodeRsp, error) {
	if req == nil {
		return &pb.GetVerifyCodeRsp{Code: 1, Message: "request is required"}, nil
	}
	pattern := `^1[3-9]\d{9}$`
	match, err := regexp.MatchString(pattern, strings.TrimSpace(req.Telephone))
	if err != nil {
		return &pb.GetVerifyCodeRsp{Code: 1, Message: "validate telephone failed"}, nil
	}
	if !match {
		return &pb.GetVerifyCodeRsp{Code: 1, Message: "telephone format is invalid"}, nil
	}

	reply, err := s.CustomerBiz.GetVerifyCode(ctx, 6, verifyCode.TYPE_DEFAULT)
	if err != nil {
		return &pb.GetVerifyCodeRsp{Code: 1, Message: "get verify code failed"}, nil
	}

	const lifetime = 60
	if err := s.CustomerData.SetVerifyCode(ctx, req.Telephone, reply.Code, lifetime); err != nil {
		return &pb.GetVerifyCodeRsp{Code: 1, Message: "cache verify code failed"}, nil
	}
	return &pb.GetVerifyCodeRsp{
		Code:           0,
		Message:        "verify code issued",
		VerifyCode:     reply.Code,
		VerifyCodeTime: time.Now().Unix(),
		VerifyCodeTtl:  lifetime,
	}, nil
}

func (s *CustomerService) Login(ctx context.Context, req *pb.LoginReq) (*pb.LoginRsp, error) {
	if req == nil {
		return &pb.LoginRsp{Code: 1, Message: "request is required"}, nil
	}
	storedCode, err := s.CustomerData.GetVerifyCode(ctx, req.Telephone)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return &pb.LoginRsp{Code: 1, Message: "verify code expired or missing"}, nil
		}
		return &pb.LoginRsp{Code: 1, Message: "read verify code failed"}, nil
	}
	if storedCode == "" {
		return &pb.LoginRsp{Code: 1, Message: "verify code is invalid"}, nil
	}
	if storedCode != req.VerifyCode {
		return &pb.LoginRsp{Code: 1, Message: "verify code is incorrect"}, nil
	}

	customer, err := s.CustomerData.GetCustomerByTelephone(ctx, req.Telephone)
	if err != nil {
		return &pb.LoginRsp{Code: 1, Message: "create or get customer failed"}, nil
	}

	secretKey := ""
	var ttlSeconds int32 = 3600
	if s.security != nil {
		secretKey = s.security.GetJwtSecret()
		if ts := s.security.GetJwtTtlSeconds(); ts > 0 {
			ttlSeconds = ts
		}
	}
	if secretKey == "" {
		return &pb.LoginRsp{Code: 1, Message: "jwt secret is missing"}, nil
	}

	token, issuedAt, err := s.CustomerData.GenerateTokenAndSave(ctx, customer, time.Duration(ttlSeconds)*time.Second, secretKey)
	if err != nil {
		return &pb.LoginRsp{Code: 1, Message: tokenSaveFailureHint(err)}, nil
	}

	return &pb.LoginRsp{
		Code:          0,
		Message:       "login success",
		Token:         token,
		TokenCreateAt: issuedAt,
		TokenTtl:      ttlSeconds,
	}, nil
}

func tokenSaveFailureHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "data too long") {
		return "token column is too short"
	}
	if strings.Contains(msg, "jwt secret") || strings.Contains(msg, "customer id required") {
		return "token signing config is invalid"
	}
	return "save login token failed"
}

func (s *CustomerService) Logout(ctx context.Context, req *pb.LogoutReq) (*pb.LogoutRsp, error) {
	customerID, err := customerIDFromContext(ctx)
	if err != nil {
		return &pb.LogoutRsp{Code: 1, Message: "unauthorized, please login first"}, nil
	}
	if err := s.CustomerData.DeleteToken(ctx, strconv.FormatUint(customerID, 10)); err != nil {
		return &pb.LogoutRsp{Code: 1, Message: "logout failed"}, nil
	}
	return &pb.LogoutRsp{Code: 0, Message: "logout success"}, nil
}

func (s *CustomerService) GetEstimatePrice(ctx context.Context, req *pb.GetEstimatePriceReq) (*pb.GetEstimatePriceRsp, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_ESTIMATE_PRICE_REQUEST", "request is required")
	}
	ctx, err := customerMetadataContext(ctx)
	if err != nil {
		return nil, err
	}
	estimate, err := s.CustomerBiz.GetEstimatePrice(ctx, strings.TrimSpace(req.Origin), strings.TrimSpace(req.Destination))
	if err != nil {
		return nil, kerrors.InternalServer("ESTIMATE_PRICE_FAILED", "estimate price failed")
	}
	return &pb.GetEstimatePriceRsp{
		Code:        estimate.Code,
		Message:     estimate.Message,
		Price:       estimate.Price,
		Origin:      estimate.Origin,
		Destination: estimate.Destination,
		Distance:    estimate.Distance,
		Duration:    estimate.Duration,
	}, nil
}

func (s *CustomerService) CreateOrder(ctx context.Context, req *pb.CreateOrderReq) (*pb.CreateOrderRsp, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_CREATE_ORDER_REQUEST", "request is required")
	}
	ctx, err := customerMetadataContext(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.CustomerBiz.CreateOrder(ctx, strings.TrimSpace(req.Origin), strings.TrimSpace(req.Destination), strings.TrimSpace(req.Remark))
	if err != nil {
		return nil, err
	}
	return &pb.CreateOrderRsp{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}, nil
}

func (s *CustomerService) GetOrder(ctx context.Context, req *pb.GetOrderReq) (*pb.GetOrderRsp, error) {
	if req == nil || req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	ctx, err := customerMetadataContext(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.CustomerBiz.GetOrder(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	return &pb.GetOrderRsp{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}, nil
}

func (s *CustomerService) ListCustomerOrders(ctx context.Context, req *pb.ListCustomerOrdersReq) (*pb.ListCustomerOrdersRsp, error) {
	if req == nil {
		req = &pb.ListCustomerOrdersReq{}
	}
	ctx, err := customerMetadataContext(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.CustomerBiz.ListCustomerOrders(ctx, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	return &pb.ListCustomerOrdersRsp{
		Code:    reply.Code,
		Message: reply.Message,
		Orders:  convertOrderInfos(reply.Orders),
		Total:   reply.Total,
	}, nil
}

func (s *CustomerService) CancelOrder(ctx context.Context, req *pb.CancelOrderReq) (*pb.CancelOrderRsp, error) {
	if req == nil || req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	ctx, err := customerMetadataContext(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.CustomerBiz.CancelOrder(ctx, req.OrderId, strings.TrimSpace(req.Reason))
	if err != nil {
		return nil, err
	}
	return &pb.CancelOrderRsp{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}, nil
}

func customerMetadataContext(ctx context.Context) (context.Context, error) {
	customerID, err := customerIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return metadata.AppendToOutgoingContext(
		ctx,
		"x-user-id", strconv.FormatUint(customerID, 10),
		"x-user-role", "customer",
	), nil
}

func customerIDFromContext(ctx context.Context) (uint64, error) {
	claims, ok := jwt.FromContext(ctx)
	if !ok {
		return 0, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}
	claimsMap, ok := claims.(jwtv5.MapClaims)
	if !ok {
		return 0, kerrors.Unauthorized("INVALID_TOKEN_CLAIMS", "invalid token claims")
	}
	customerID, ok := claimsMap["sub"].(string)
	if !ok || strings.TrimSpace(customerID) == "" {
		return 0, kerrors.Unauthorized("INVALID_CUSTOMER_ID", "invalid customer id")
	}
	id, err := strconv.ParseUint(customerID, 10, 64)
	if err != nil || id == 0 {
		return 0, kerrors.Unauthorized("INVALID_CUSTOMER_ID", "invalid customer id")
	}
	return id, nil
}

func convertOrderInfos(orders []*orderAPI.OrderInfo) []*pb.OrderInfo {
	infos := make([]*pb.OrderInfo, 0, len(orders))
	for _, order := range orders {
		infos = append(infos, convertOrderInfo(order))
	}
	return infos
}

func convertOrderInfo(order *orderAPI.OrderInfo) *pb.OrderInfo {
	if order == nil {
		return nil
	}
	return &pb.OrderInfo{
		OrderId:       order.OrderId,
		OrderNo:       order.OrderNo,
		CustomerId:    order.CustomerId,
		DriverId:      order.DriverId,
		Origin:        order.Origin,
		Destination:   order.Destination,
		Distance:      order.Distance,
		Duration:      order.Duration,
		EstimatePrice: order.EstimatePrice,
		Status:        order.Status,
		CreatedAt:     order.CreatedAt,
		AcceptedAt:    order.AcceptedAt,
		StartedAt:     order.StartedAt,
		FinishedAt:    order.FinishedAt,
		CancelledAt:   order.CancelledAt,
	}
}
