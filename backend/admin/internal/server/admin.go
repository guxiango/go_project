package server

import (
	"context"
	"strings"

	"admin/internal/biz"
	"admin/internal/service"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/transport"
)

func adminJWT(adminService *service.AdminService) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			claims, ok := jwt.FromContext(ctx)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "未授权，请先登录")
			}
			adminClaims, ok := claims.(*biz.AdminClaims)
			if !ok || adminClaims.AdminId == 0 {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "token 信息格式错误")
			}

			savedToken, err := adminService.GetSavedToken(ctx, adminClaims.AdminId)
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
			log.Context(ctx).Infof("admin_id=%d username=%s role=%s", adminClaims.AdminId, adminClaims.Username, adminClaims.Role)
			return handler(ctx, req)
		}
	}
}
