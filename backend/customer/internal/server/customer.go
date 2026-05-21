package server

import (
	"context"
	"customer/internal/service"
	"strings"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	jwt "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/go-kratos/kratos/v2/transport"
)
func customerJWT(customerService *service.CustomerService, logger log.Logger) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// 获取token中的用户ID
			claims, ok := jwt.FromContext(ctx)
			if !ok {
				// 没有获取到token，返回401错误
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "未授权，请先登录")
			}
			claimsMap := claims.(jwtv5.MapClaims)
			log.NewHelper(logger).Infof("%s", claimsMap)
			customerID, ok := claimsMap["sub"].(string)
			if !ok {
				// 没有获取到用户ID，返回401错误
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "未授权，请先登录")
			}
			// 获取token
			token, err := customerService.CustomerData.GetTokenByID(ctx, customerID)
			if err != nil {
				// 获取token失败，返回401错误
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "未授权，请先登录")
			}
			// 如果token不匹配，返回401错误
			header, _ := transport.FromServerContext(ctx)
			auths := strings.SplitN(header.RequestHeader().Get("Authorization"), " ", 2)
			jwtToken := auths[1]
			if jwtToken != token {
				// 如果token不匹配，返回401错误
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "授权过期，请重新登录")
			}
			// 如果token匹配，继续执行
			return handler(ctx, req)
		}
	}
}