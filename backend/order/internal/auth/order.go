package auth

import (
	"context"
	"strconv"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"google.golang.org/grpc/metadata"
)

type contextKey struct{}
type User struct {
	ID   uint64
	Role string
}

// Middleware for obtaining identity information
func Middleware() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, kratoserrors.Unauthorized("AUTH_MISSING", "auth metadata missing")
			}
			ids := md.Get("x-user-id")
			roles := md.Get("x-user-role")
			if len(ids) == 0 || len(roles) == 0 {
				return nil, kratoserrors.Unauthorized("AUTH_MISSING", "user id or role missing")
			}
			id, err := strconv.ParseUint(ids[0], 10, 64)
			if err != nil || id == 0 {
				return nil, kratoserrors.Unauthorized("INVALID_USER_ID", "invalid user id")
			}

			user := &User{
				ID:   id,
				Role: roles[0],
			}

			ctx = context.WithValue(ctx, contextKey{}, user)
			return handler(ctx, req)
		}
	}
}

// FromContext Obtained identity information
func FromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(contextKey{}).(*User)
	return user, ok
}
