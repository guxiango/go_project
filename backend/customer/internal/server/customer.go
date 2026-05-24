package server

import (
	"context"
	"strings"

	"customer/internal/service"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	jwt "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/transport"
	jwtv5 "github.com/golang-jwt/jwt/v5"
)

func customerJWT(customerService *service.CustomerService, logger log.Logger) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			claims, ok := jwt.FromContext(ctx)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
			}
			claimsMap, ok := claims.(jwtv5.MapClaims)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "invalid token claims")
			}
			log.NewHelper(logger).Infof("%s", claimsMap)
			customerID, ok := claimsMap["sub"].(string)
			if !ok || strings.TrimSpace(customerID) == "" {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
			}

			token, err := customerService.CustomerData.GetTokenByID(ctx, customerID)
			if err != nil {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
			}

			header, ok := transport.FromServerContext(ctx)
			if !ok {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "authorization header missing")
			}
			auths := strings.SplitN(header.RequestHeader().Get("Authorization"), " ", 2)
			if len(auths) != 2 || !strings.EqualFold(auths[0], "Bearer") || auths[1] == "" {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "authorization header invalid")
			}
			if auths[1] != token {
				return nil, kerrors.Unauthorized("UNAUTHORIZED", "authorization expired")
			}
			return handler(ctx, req)
		}
	}
}
