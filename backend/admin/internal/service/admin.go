package service

import (
	"context"

	pb "admin/api/admin"
	"admin/internal/biz"

	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
)

type AdminService struct {
	pb.UnimplementedAdminServer
	uc *biz.AdminUsecase
}

func NewAdminService(uc *biz.AdminUsecase) *AdminService {
	return &AdminService{uc: uc}
}

func (s *AdminService) Login(ctx context.Context, req *pb.AdminLoginRequest) (*pb.AdminLoginReply, error) {
	admin, token, ttl, err := s.uc.Login(ctx, req.Username, req.Password)
	if err != nil {
		return &pb.AdminLoginReply{Code: 1, Message: err.Error()}, nil
	}
	return &pb.AdminLoginReply{
		Code:          0,
		Message:       "登录成功",
		Token:         token,
		TokenCreateAt: admin.TokenCreateAt.Time.Unix(),
		TokenTtl:      int32(ttl),
		Role:          admin.Role,
	}, nil
}

func (s *AdminService) AuditDriverProfile(ctx context.Context, req *pb.AuditDriverProfileRequest) (*pb.AuditDriverProfileReply, error) {
	claims, ok := adminClaimsFromContext(ctx)
	if !ok {
		return &pb.AuditDriverProfileReply{Code: 1, Message: "未授权，请先登录"}, nil
	}
	if !s.uc.CanAudit(claims.Role) {
		return &pb.AuditDriverProfileReply{Code: 1, Message: "无司机审核权限"}, nil
	}
	status, message, code, err := s.uc.AuditDriverProfile(ctx, claims.AdminId, req.DriverId, req.Approved, req.Reason)
	if err != nil {
		return &pb.AuditDriverProfileReply{Code: 1, Message: err.Error()}, nil
	}
	return &pb.AuditDriverProfileReply{Code: code, Message: message, Status: status}, nil
}

func (s *AdminService) ListPendingDrivers(ctx context.Context, req *pb.ListPendingDriversRequest) (*pb.ListPendingDriversReply, error) {
	claims, ok := adminClaimsFromContext(ctx)
	if !ok {
		return &pb.ListPendingDriversReply{Code: 1, Message: "未授权，请先登录"}, nil
	}
	if !s.uc.CanRead(claims.Role) {
		return &pb.ListPendingDriversReply{Code: 1, Message: "无后台查看权限"}, nil
	}
	items, total, message, code, err := s.uc.ListPendingDrivers(ctx, req.Page, req.PageSize)
	if err != nil {
		return &pb.ListPendingDriversReply{Code: 1, Message: err.Error()}, nil
	}
	drivers := make([]*pb.PendingDriver, 0, len(items))
	for _, item := range items {
		drivers = append(drivers, &pb.PendingDriver{
			DriverId:      item.DriverID,
			Name:          item.Name,
			Telephone:     item.Telephone,
			Status:        item.Status,
			IdNumber:      item.IDNumber,
			IdImageA:      item.IDImageA,
			IdImageB:      item.IDImageB,
			LicenseImageA: item.LicenseImageA,
			LicenseImageB: item.LicenseImageB,
			DistinctCode:  item.DistinctCode,
			UpdatedAt:     item.UpdatedAt,
		})
	}
	return &pb.ListPendingDriversReply{Code: code, Message: message, Total: total, Drivers: drivers}, nil
}

func (s *AdminService) GetSavedToken(ctx context.Context, adminID uint) (string, error) {
	return s.uc.GetAdminToken(ctx, adminID)
}

func adminClaimsFromContext(ctx context.Context) (*biz.AdminClaims, bool) {
	claims, ok := jwt.FromContext(ctx)
	if !ok {
		return nil, false
	}
	adminClaims, ok := claims.(*biz.AdminClaims)
	if !ok || adminClaims.AdminId == 0 {
		return nil, false
	}
	return adminClaims, true
}
