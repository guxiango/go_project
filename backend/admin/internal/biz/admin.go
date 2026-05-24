package biz

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	driverpb "admin/api/driver"
	"admin/internal/conf"

	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	jwtv5 "github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gorm.io/gorm"
)

const (
	RoleSuperAdmin = "super_admin"
	RoleAuditor    = "auditor"
	RoleOperator   = "operator"

	AdminStatusActive  = "active"
	AdminStatusBlocked = "blocked"
)

type AdminClaims struct {
	AdminId  uint   `json:"admin_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwtv5.RegisteredClaims
}

type Admin struct {
	gorm.Model
	Username      string         `gorm:"type:varchar(64);uniqueIndex"`
	Password      string         `gorm:"type:varchar(255)"`
	Role          string         `gorm:"type:varchar(32);index"`
	Status        string         `gorm:"type:varchar(32);index"`
	Token         sql.NullString `gorm:"type:varchar(512)"`
	TokenCreateAt sql.NullTime   `gorm:"type:datetime"`
}

type DriverProfileAuditLog struct {
	gorm.Model
	DriverID uint
	AdminID  uint
	Approved bool
	Reason   sql.NullString
	Status   string
}

type PendingDriver struct {
	DriverID      uint64
	Name          string
	Telephone     string
	Status        string
	IDNumber      string
	IDImageA      string
	IDImageB      string
	LicenseImageA string
	LicenseImageB string
	DistinctCode  string
	UpdatedAt     int64
}

type AdminRepo interface {
	FindByUsername(context.Context, string) (*Admin, error)
	FindByID(context.Context, uint) (*Admin, error)
	SaveToken(context.Context, *Admin, string, time.Time) error
	CreateAuditLog(context.Context, *DriverProfileAuditLog) error
}

type DriverClient interface {
	AuditDriverProfile(context.Context, uint64, uint64, bool, string) (string, string, int64, error)
	ListPendingDrivers(context.Context, int32, int32) ([]*PendingDriver, int64, string, int64, error)
}

type AdminUsecase struct {
	repo      AdminRepo
	driver    DriverClient
	security  *conf.Security
	secretKey string
	ttl       time.Duration
}

func NewAdminUsecase(repo AdminRepo, driver DriverClient, security *conf.Security) *AdminUsecase {
	ttlSeconds := int64(7200)
	secret := ""
	if security != nil && security.Jwt != nil {
		secret = security.Jwt.Secret
		if security.Jwt.TtlSeconds > 0 {
			ttlSeconds = security.Jwt.TtlSeconds
		}
	}
	return &AdminUsecase{
		repo:      repo,
		driver:    driver,
		security:  security,
		secretKey: secret,
		ttl:       time.Duration(ttlSeconds) * time.Second,
	}
}

func (uc *AdminUsecase) Login(ctx context.Context, username, password string) (*Admin, string, int64, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, "", 0, errors.New("username and password are required")
	}
	if uc.secretKey == "" {
		return nil, "", 0, errors.New("admin jwt secret is not configured")
	}
	admin, err := uc.repo.FindByUsername(ctx, username)
	if err != nil {
		return nil, "", 0, err
	}
	if admin.Status != AdminStatusActive {
		return nil, "", 0, errors.New("admin account is not active")
	}
	if !CheckPassword(admin.Password, password) {
		return nil, "", 0, errors.New("invalid username or password")
	}

	now := time.Now()
	claims := &AdminClaims{
		AdminId:  admin.ID,
		Username: admin.Username,
		Role:     admin.Role,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    "laomadj_admin",
			Subject:   "AdminToken",
			ExpiresAt: jwtv5.NewNumericDate(now.Add(uc.ttl)),
			IssuedAt:  jwtv5.NewNumericDate(now),
		},
	}
	token, err := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims).SignedString([]byte(uc.secretKey))
	if err != nil {
		return nil, "", 0, err
	}
	if err := uc.repo.SaveToken(ctx, admin, token, now); err != nil {
		return nil, "", 0, err
	}
	admin.TokenCreateAt = sql.NullTime{Time: now, Valid: true}
	return admin, token, int64(uc.ttl.Seconds()), nil
}

func (uc *AdminUsecase) AuditDriverProfile(ctx context.Context, adminID uint, driverID uint64, approved bool, reason string) (string, string, int64, error) {
	if driverID == 0 {
		return "", "", 1, errors.New("driver_id is required")
	}
	status, message, code, err := uc.driver.AuditDriverProfile(ctx, driverID, uint64(adminID), approved, reason)
	if err != nil {
		return "", "", 1, err
	}
	_ = uc.repo.CreateAuditLog(ctx, &DriverProfileAuditLog{
		DriverID: uint(driverID),
		AdminID:  adminID,
		Approved: approved,
		Reason:   sql.NullString{String: reason, Valid: strings.TrimSpace(reason) != ""},
		Status:   status,
	})
	return status, message, code, nil
}

func (uc *AdminUsecase) ListPendingDrivers(ctx context.Context, page, pageSize int32) ([]*PendingDriver, int64, string, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return uc.driver.ListPendingDrivers(ctx, page, pageSize)
}

func (uc *AdminUsecase) GetAdminToken(ctx context.Context, id uint) (string, error) {
	admin, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if !admin.Token.Valid || admin.Token.String == "" {
		return "", errors.New("admin token is empty")
	}
	return admin.Token.String, nil
}

func (uc *AdminUsecase) CanAudit(role string) bool {
	return role == RoleSuperAdmin || role == RoleAuditor
}

func (uc *AdminUsecase) CanRead(role string) bool {
	return role == RoleSuperAdmin || role == RoleAuditor || role == RoleOperator
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func CheckPassword(stored, password string) bool {
	if strings.HasPrefix(stored, "sha256:") {
		expected := HashPassword(password)
		return subtle.ConstantTimeCompare([]byte(stored), []byte(expected)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(password)) == 1
}

type DriverBiz struct {
	client        driverpb.DriverClient
	conn          *grpc.ClientConn
	internalToken string
}

func NewDriverBiz(reg *consul.Registry, c *conf.Service) (*DriverBiz, func(), error) {
	endpoint := "discovery:///Driver"
	internalToken := ""
	if c != nil && c.Driver != nil {
		if c.Driver.Endpoint != "" {
			endpoint = c.Driver.Endpoint
		}
		internalToken = c.Driver.InternalToken
	}
	conn, err := kgrpc.DialInsecure(context.Background(),
		kgrpc.WithDiscovery(reg),
		kgrpc.WithEndpoint(endpoint),
		kgrpc.WithMiddleware(tracing.Client()),
		kgrpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = conn.Close() }
	return &DriverBiz{
		client:        driverpb.NewDriverClient(conn),
		conn:          conn,
		internalToken: internalToken,
	}, cleanup, nil
}

func (d *DriverBiz) withInternalToken(ctx context.Context) context.Context {
	if d.internalToken == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-internal-token", d.internalToken)
}

func (d *DriverBiz) AuditDriverProfile(ctx context.Context, driverID, adminID uint64, approved bool, reason string) (string, string, int64, error) {
	reply, err := d.client.InternalAuditDriverProfile(d.withInternalToken(ctx), &driverpb.InternalAuditDriverProfileRequest{
		DriverId: driverID,
		AdminId:  adminID,
		Approved: approved,
		Reason:   reason,
	})
	if err != nil {
		return "", "", 1, err
	}
	return reply.Status, reply.Message, reply.Code, nil
}

func (d *DriverBiz) ListPendingDrivers(ctx context.Context, page, pageSize int32) ([]*PendingDriver, int64, string, int64, error) {
	reply, err := d.client.InternalListPendingDrivers(d.withInternalToken(ctx), &driverpb.InternalListPendingDriversRequest{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, 0, "", 1, err
	}
	drivers := make([]*PendingDriver, 0, len(reply.Drivers))
	for _, item := range reply.Drivers {
		drivers = append(drivers, &PendingDriver{
			DriverID:      item.DriverId,
			Name:          item.Name,
			Telephone:     item.Telephone,
			Status:        item.Status,
			IDNumber:      item.IdNumber,
			IDImageA:      item.IdImageA,
			IDImageB:      item.IdImageB,
			LicenseImageA: item.LicenseImageA,
			LicenseImageB: item.LicenseImageB,
			DistinctCode:  item.DistinctCode,
			UpdatedAt:     item.UpdatedAt,
		})
	}
	return drivers, reply.Total, reply.Message, reply.Code, nil
}
