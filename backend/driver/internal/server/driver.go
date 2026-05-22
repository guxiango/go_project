package server

import (
	"context"
	"driver/internal/biz"
	"driver/internal/service"
	"strings"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/transport"
)

func driverJWT(driverService *service.DriverService) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			claims, ok := jwt.FromContext(ctx)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "未授权，请先登录")
			}
			driverClaims, ok := claims.(*biz.DriverClaims)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "token信息格式错误")
			}

			driverID := driverClaims.DriverId
			driverName := driverClaims.DriverName
			log.Context(ctx).Infof("driver_id=%d driver_name=%s", driverID, driverName)

			savedToken, err := driverService.DriverData.GetTokenByID(ctx, driverID)
			if err != nil {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "授权已失效，请重新登录")
			}

			header, ok := transport.FromServerContext(ctx)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "请求上下文错误")
			}
			auths := strings.SplitN(header.RequestHeader().Get("Authorization"), " ", 2)
			if len(auths) != 2 || !strings.EqualFold(auths[0], "Bearer") {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "请求授权格式错误")
			}
			if auths[1] != savedToken {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "授权已过期，请重新登录")
			}

			return handler(ctx, req)
		}
	}
}
