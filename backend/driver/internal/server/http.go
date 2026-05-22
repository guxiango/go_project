package server

import (
	"context"
	driverAPI "driver/api/driver"
	v1 "driver/api/helloworld/v1"
	"driver/internal/biz"
	"driver/internal/conf"
	"driver/internal/service"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"
	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, greeter *service.GreeterService, sec *conf.Security, driver *service.DriverService, logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			selector.Server(jwt.Server(func(token *jwtv5.Token) (interface{}, error) {
				if sec == nil || sec.GetJwt().GetSecret() == "" {
					return nil, kerrors.InternalServer("JWT_SECRET_MISSING", "JWT secret is not configured")
				}
				return []byte(sec.GetJwt().GetSecret()), nil
			}, jwt.WithClaims(func() jwtv5.Claims {
				return &biz.DriverClaims{}
			})), driverJWT(driver)).Match(func(ctx context.Context, operation string) bool {
				publicOperations := map[string]bool{
					driverAPI.OperationDriverGetVerifyCode: true,
					driverAPI.OperationDriverRegister:      true,
					driverAPI.OperationDriverLogin:         true,
				}
				return !publicOperations[operation]
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
	v1.RegisterGreeterHTTPServer(srv, greeter)
	driverAPI.RegisterDriverHTTPServer(srv, driver)
	return srv
}
