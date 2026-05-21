package server

import (
	"context"

	customer "customer/api/customer"
	v1 "customer/api/helloworld/v1"
	"customer/internal/conf"
	"customer/internal/service"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	jwt "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"
	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, sec *conf.Security, customerService *service.CustomerService, greeter *service.GreeterService, logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			// 添加JWT认证中间件
			selector.Server(jwt.Server(func(token *jwtv5.Token) (interface{}, error) {
				if sec == nil || sec.GetJwtSecret() == "" {
					return nil, kerrors.InternalServer("JWT_SECRET_MISSING", "JWT 密钥未配置")
				}
				return []byte(sec.GetJwtSecret()), nil
			}), customerJWT(customerService, logger)).Match(func(ctx context.Context, operation string) bool {
				log.NewHelper(logger).Infof("operation: %s", operation)
				noJWT := map[string]bool{
					"/api.customer.Customer/Login":         true,
					"/api.customer.Customer/GetVerifyCode": true,
				}
				if _, ok := noJWT[operation]; ok {
					return false
				}
				return true
			}).Build(),
		),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	customer.RegisterCustomerHTTPServer(srv, customerService)
	v1.RegisterGreeterHTTPServer(srv, greeter)
	return srv
}
