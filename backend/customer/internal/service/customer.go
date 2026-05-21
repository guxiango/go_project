package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	pb "customer/api/customer"
	verifyCode "verify-code/api/verifyCode"

	"github.com/redis/go-redis/v9"

	"customer/internal/conf"
	customerData "customer/internal/data"

	jwt "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	jwtv5 "github.com/golang-jwt/jwt/v5"
	customerBiz "customer/internal/biz"
)

type CustomerService struct {
	pb.UnimplementedCustomerServer
	CustomerData *customerData.CustomerData
	security     *conf.Security
	CustomerBiz *customerBiz.CustomerBiz
}

func NewCustomerService(customerData *customerData.CustomerData, security *conf.Security, customerBiz *customerBiz.CustomerBiz) *CustomerService {
	return &CustomerService{
		CustomerData: customerData,
		security:     security,
		CustomerBiz:  customerBiz,
	}
}

func (s *CustomerService) GetVerifyCode(ctx context.Context, req *pb.GetVerifyCodeReq) (*pb.GetVerifyCodeRsp, error) {
	// 1.验证手机号是否合法
	pattern := `^1[3-9]\d{9}$`
	match, err := regexp.MatchString(pattern, req.Telephone)
	if err != nil {
		return &pb.GetVerifyCodeRsp{
			Code:    1,
			Message: "校验手机号时发生异常，请稍后重试",
		}, nil
	}
	if !match {
		return &pb.GetVerifyCodeRsp{
			Code:    1,
			Message: "手机号格式不正确，请输入11位中国大陆手机号",
		}, nil
	}
	// 2. 调用 verify-code 服务获取验证码
	reply, err := s.CustomerBiz.GetVerifyCode(ctx, 6, verifyCode.TYPE_DEFAULT)
	if err != nil {
		return &pb.GetVerifyCodeRsp{
			Code:    1,
			Message: "获取验证码失败，请稍后重试",
		}, nil
	}
	// 3.向redis中设置验证码
	const lifetime = 60
	err = s.CustomerData.SetVerifyCode(ctx, req.Telephone, reply.Code, lifetime)
	if err != nil {
		return &pb.GetVerifyCodeRsp{
			Code:    1,
			Message: "验证码暂存失败（缓存不可用），请稍后重试",
		}, nil
	}
	return &pb.GetVerifyCodeRsp{
		Code:           0,
		Message:        "验证码已下发，请在有效期内填写",
		VerifyCode:     reply.Code,
		VerifyCodeTime: time.Now().Unix(),
		VerifyCodeTtl:  lifetime,
	}, nil
}

func (s *CustomerService) Login(ctx context.Context, req *pb.LoginReq) (*pb.LoginRsp, error) {
	// 验证手机号和验证码是否正确
	storedCode, err := s.CustomerData.GetVerifyCode(ctx, req.Telephone)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return &pb.LoginRsp{
				Code:    1,
				Message: "验证码已过期或不存在，请先获取验证码",
			}, nil
		}
		return &pb.LoginRsp{
			Code:    1,
			Message: "读取验证码失败，请稍后重试",
		}, nil
	}
	if storedCode == "" {
		return &pb.LoginRsp{
			Code:    1,
			Message: "验证码已失效，请先重新获取验证码",
		}, nil
	}
	if storedCode != req.VerifyCode {
		return &pb.LoginRsp{
			Code:    1,
			Message: "验证码不正确，请核对后重新输入",
		}, nil
	}
	// 判断手机号是否注册, 来获取顾客信息
	customer, err := s.CustomerData.GetCustomerByTelephone(ctx, req.Telephone)
	if err != nil {
		return &pb.LoginRsp{
			Code:    1,
			Message: "创建或查询账户失败，请稍后重试",
		}, nil
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
		return &pb.LoginRsp{
			Code:    1,
			Message: "登录服务未配置密钥（jwt_secret），请联系管理员",
		}, nil
	}
	token, issuedAt, err := s.CustomerData.GenerateTokenAndSave(ctx, customer, time.Duration(ttlSeconds)*time.Second, secretKey)
	if err != nil {
		return &pb.LoginRsp{
			Code:    1,
			Message: tokenSaveFailureHint(err),
		}, nil
	}

	return &pb.LoginRsp{
		Code:          0,
		Message:       "登录成功，请妥善保管令牌并在请求头中携带",
		Token:         token,
		TokenCreateAt: issuedAt,
		TokenTtl:      ttlSeconds,
	}, nil
}

// tokenSaveFailureHint 将签发/落库错误转译为前端可读说明（不暴露内部栈信息）。
func tokenSaveFailureHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "data too long") {
		return "登录令牌无法写入数据库（字段长度不足），请联系管理员调整 token 列"
	}
	if strings.Contains(msg, "jwt secret") || strings.Contains(msg, "customer id required") {
		return "登录令牌签发配置异常，请联系管理员"
	}
	return "登录令牌签发或保存失败，请稍后重试"
}

// 登出服务
func (s *CustomerService) Logout(ctx context.Context, req *pb.LogoutReq) (*pb.LogoutRsp, error) {
	// 获取token中的用户ID
	claims, ok := jwt.FromContext(ctx)
	if !ok {
		return &pb.LogoutRsp{
			Code:    1,
			Message: "未授权，请先登录",
		}, nil
	}
	claimsMap := claims.(jwtv5.MapClaims)
	customerID, ok := claimsMap["sub"].(string)
	if !ok {
		return &pb.LogoutRsp{
			Code:    1,
			Message: "未授权，请先登录",
		}, nil
	}
	// 删除token
	err := s.CustomerData.DeleteToken(ctx, customerID)
	if err != nil {
		return &pb.LogoutRsp{
			Code:    1,
			Message: "登出失败，请稍后重试",
		}, nil
	}
	return &pb.LogoutRsp{
		Code:    0,
		Message: "登出成功",
	}, nil

}

func (s *CustomerService) GetEstimatePrice(ctx context.Context, req *pb.GetEstimatePriceReq) (*pb.GetEstimatePriceRsp, error) {
	price, err := s.CustomerBiz.GetEstimatePrice(ctx, req.Origin, req.Destination)
	if err != nil {
		return &pb.GetEstimatePriceRsp{
			Code:    1,
			Message: "价格预估失败，请稍后重试",
		}, nil
	}
	return &pb.GetEstimatePriceRsp{
		Code:    0,
		Message: "价格预估成功",
		Price:   price,
	}, nil
}