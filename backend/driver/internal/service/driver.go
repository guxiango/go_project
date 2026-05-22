package service

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"gorm.io/gorm"

	pb "driver/api/driver"
	verifyCode "driver/api/verifyCode"
	driverBiz "driver/internal/biz"
	"driver/internal/conf"
	driverData "driver/internal/data"
)

var phoneNumberPattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

type DriverService struct {
	pb.UnimplementedDriverServer
	DriverData *driverData.DriverData
	DriverBiz  *driverBiz.DriverBiz
	Security   *conf.Security
}

func NewDriverService(driverData *driverData.DriverData, driverBiz *driverBiz.DriverBiz, security *conf.Security) *DriverService {
	return &DriverService{DriverData: driverData, DriverBiz: driverBiz, Security: security}
}

func (s *DriverService) GetVerifyCode(ctx context.Context, req *pb.GetVerifyCodeRequest) (*pb.GetVerifyCodeReply, error) {
	if !isValidPhoneNumber(req.Telephone) {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "手机号格式不正确，请输入11位中国大陆手机号",
		}, nil
	}

	reply, err := s.DriverBiz.GetVerifyCode(ctx, req.Telephone, 6, verifyCode.TYPE_DEFAULT)
	if err != nil {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "获取验证码失败，请稍后重试",
		}, nil
	}

	const lifetime = 60
	err = s.DriverData.SetVerifyCode(ctx, req.Telephone, reply.Code, lifetime)
	if err != nil {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "验证码暂存失败，请稍后重试",
		}, nil
	}
	return &pb.GetVerifyCodeReply{
		Code:           0,
		Message:        "验证码已下发，请在有效期内填写",
		VerifyCode:     reply.Code,
		VerifyCodeTime: time.Now().Unix(),
		VerifyCodeTtl:  lifetime,
	}, nil
}

// 注册
func (s *DriverService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterReply, error) {
	if ok, message := s.validatePhoneAndVerifyCode(ctx, req.Telephone, req.VerifyCode); !ok {
		return &pb.RegisterReply{
			Code:    1,
			Message: message,
		}, nil
	}
	// 2.判断司机是否已经注册，没注册则注册
	driver, err := s.DriverData.GetDriverByPhone(ctx, req.Telephone)
	if err != nil && err != gorm.ErrRecordNotFound {
		return &pb.RegisterReply{
			Code:    1,
			Message: "查询司机信息失败，请稍后重试",
		}, nil
	}
	if driver != nil {
		return &pb.RegisterReply{
			Code:    1,
			Message: "改手机号已经注册，请直接登录",
		}, nil
	}
	createdDriver, err := s.DriverData.CreateDriver(ctx, req.Telephone)
	if err != nil {
		return &pb.RegisterReply{
			Code:    1,
			Message: "注册失败，请稍后重试",
		}, nil
	}
	return &pb.RegisterReply{
		Code:     0,
		Status:   driverBiz.DriverStatusStop,
		DriverId: uint64(createdDriver.ID),
		Message:  "注册成功",
	}, nil
}

// Login 登录
func (s *DriverService) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginReply, error) {
	if ok, message := s.validatePhoneAndVerifyCode(ctx, req.Telephone, req.VerifyCode); !ok {
		return &pb.LoginReply{
			Code:    1,
			Message: message,
		}, nil
	}
	driver, err := s.DriverData.GetDriverByPhone(ctx, req.Telephone)
	if err != nil {
		return &pb.LoginReply{
			Code:    1,
			Message: "查询司机信息失败，请稍后重试",
		}, nil
	}
	if driver == nil {
		return &pb.LoginReply{
			Code:    1,
			Message: "该手机号未注册，请先注册",
		}, nil
	}
	if driver.Status.Valid && driver.Status.String == driverBiz.DriverStatusBlocked {
		return &pb.LoginReply{
			Code:    1,
			Message: "您的账号已被封禁，请联系客服",
		}, nil
	}
	// 生成token并更新到数据库
	secretKey := s.Security.Jwt.Secret
	if secretKey == "" {
		return &pb.LoginReply{
			Code:    1,
			Message: "登录服务未配置密钥",
		}, nil
	}
	ttl := time.Duration(s.Security.Jwt.TtlSeconds) * time.Second
	token, err := s.DriverData.GenerateTokenAndSave(ctx, driver, ttl, secretKey)
	if err != nil {
		return &pb.LoginReply{
			Code:    1,
			Message: "登录失败，请稍后重试",
		}, nil
	}
	return &pb.LoginReply{
		Code:          0,
		TokenCreateAt: driver.TokenCreateAt.Time.Unix(),
		TokenTtl:      int32(s.Security.Jwt.TtlSeconds),
		Status:        driver.Status.String,
		DriverId:      uint64(driver.ID),
		Message:       "登录成功",
		Token:         token,
	}, nil
}

// 校验手机号和验证码是否合法
func (s *DriverService) validatePhoneAndVerifyCode(ctx context.Context, phone string, verifyCode string) (bool, string) {
	if !isValidPhoneNumber(phone) {
		return false, "手机号格式不正确，请输入11位中国大陆手机号"
	}
	storedCode, err := s.DriverData.GetVerifyCode(ctx, phone)
	if err != nil {
		return false, "验证码已过期或不存在，请先获取验证码"
	}
	if storedCode == "" {
		return false, "验证码已失效，请先重新获取验证码"
	}
	if storedCode != verifyCode {
		return false, "验证码不正确，请重新输入"
	}
	return true, ""
}

// 校验手机号是否合法
func isValidPhoneNumber(phone string) bool {
	return phoneNumberPattern.MatchString(phone)
}

// 更新司机的资料
func (s *DriverService) UpdateDriverProfile(ctx context.Context, req *pb.UpdateDriverProfileRequest) (*pb.UpdateDriverProfileReply, error) {
	driverId, ok := getDriverIDFromContext(ctx)
	if !ok {
		return &pb.UpdateDriverProfileReply{
			Code:    1,
			Message: "未授权，请先登录",
		}, nil
	}
	// 使用driverId更新当前司机的资料
	if message := validateDriverProfile(req); message != "" {
		return &pb.UpdateDriverProfileReply{
			Code:    1,
			Message: message,
		}, nil
	}
	currentDriver, err := s.DriverData.GetDriverByID(ctx, driverId)
	if err != nil {
		return &pb.UpdateDriverProfileReply{
			Code:    1,
			Message: "获取司机信息失败",
		}, nil
	}
	profile := driverBiz.DriverWork{
		Name: sql.NullString{
			String: req.Name,
			Valid:  req.Name != "",
		},
		IdNumber: sql.NullString{
			String: req.IdNumber,
			Valid:  req.IdNumber != "",
		},
		IdImageA: sql.NullString{
			String: req.IdImageA,
			Valid:  req.IdImageA != "",
		},
		IdImageB: sql.NullString{
			String: req.IdImageB,
			Valid:  req.IdImageB != "",
		},
		LicenseImageA: sql.NullString{
			String: req.LicenseImageA,
			Valid:  req.LicenseImageA != "",
		},
		LicenseImageB: sql.NullString{
			String: req.LicenseImageB,
			Valid:  req.LicenseImageB != "",
		},
		DistinctCode: sql.NullString{
			String: req.DistinctCode,
			Valid:  req.DistinctCode != "",
		},
		TelephoneBak: sql.NullString{
			String: req.TelephoneBak,
			Valid:  req.TelephoneBak != "",
		},
	}
	if shouldSubmitProfileForAudit(currentDriver, profile) {
		profile.Status = sql.NullString{
			String: driverBiz.DriverStatusPending,
			Valid:  true,
		}
	}
	if err := s.DriverData.UpdateDriverProfileByID(ctx, driverId, profile); err != nil {
		return &pb.UpdateDriverProfileReply{
			Code:    1,
			Message: "更新资料失败，请稍后重试",
		}, nil
	}
	return &pb.UpdateDriverProfileReply{
		Code:    0,
		Message: "成功更新资料",
	}, nil
}

func validateDriverProfile(req *pb.UpdateDriverProfileRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "姓名不能为空"
	}
	if strings.TrimSpace(req.IdNumber) == "" {
		return "身份证号不能为空"
	}
	if strings.TrimSpace(req.IdImageA) == "" || strings.TrimSpace(req.IdImageB) == "" {
		return "身份证照片不能为空"
	}
	if strings.TrimSpace(req.LicenseImageA) == "" || strings.TrimSpace(req.LicenseImageB) == "" {
		return "驾驶证照片不能为空"
	}
	return ""
}

func shouldSubmitProfileForAudit(currentDriver *driverBiz.Driver, profile driverBiz.DriverWork) bool {
	if currentDriver == nil || !currentDriver.Status.Valid || currentDriver.Status.String == driverBiz.DriverStatusStop {
		return true
	}
	return nullStringChanged(currentDriver.IdNumber, profile.IdNumber) ||
		nullStringChanged(currentDriver.IdImageA, profile.IdImageA) ||
		nullStringChanged(currentDriver.IdImageB, profile.IdImageB) ||
		nullStringChanged(currentDriver.LicenseImageA, profile.LicenseImageA) ||
		nullStringChanged(currentDriver.LicenseImageB, profile.LicenseImageB)
}

func nullStringChanged(oldValue, newValue sql.NullString) bool {
	return oldValue.Valid != newValue.Valid || oldValue.String != newValue.String
}

func getDriverIDFromContext(ctx context.Context) (uint, bool) {
	claims, ok := jwt.FromContext(ctx)
	if !ok {
		return 0, false
	}
	driverClaims, ok := claims.(*driverBiz.DriverClaims)
	if !ok || driverClaims.DriverId == 0 {
		return 0, false
	}
	return driverClaims.DriverId, true
}

// 修改司机的状态
func (s *DriverService) UpdateWorkStatus(ctx context.Context, req *pb.UpdateWorkStatusRequest) (*pb.UpdateWorkStatusReply, error) {
	driverId, ok := getDriverIDFromContext(ctx)
	if !ok {
		return &pb.UpdateWorkStatusReply{
			Code:    1,
			Message: "未授权，请先登录",
		}, nil
	}
	targetStatus := req.Status
	if !isDriverEditableStatus(targetStatus) {
		return &pb.UpdateWorkStatusReply{
			Code:    1,
			Message: "不支持的状态",
		}, nil
	}
	driver, err := s.DriverData.GetDriverByID(ctx, driverId)
	if err != nil {
		return &pb.UpdateWorkStatusReply{
			Code:    1,
			Message: "获取司机信息失败",
		}, nil
	}
	currentStatus := ""
	if driver.Status.Valid {
		currentStatus = driver.Status.String
	}
	if !canChangeWorkStatus(currentStatus, targetStatus) {
		return &pb.UpdateWorkStatusReply{
			Code:    1,
			Message: "当前状态不允许切换到目标状态",
		}, nil
	}
	if err := s.DriverData.UpdateDriverStatusByID(ctx, driverId, currentStatus, targetStatus); err != nil {
		return &pb.UpdateWorkStatusReply{
			Code:    1,
			Message: "更新状态失败",
		}, nil
	}
	return &pb.UpdateWorkStatusReply{
		Code:    0,
		Message: "更新状态成功",
		Status:  targetStatus,
	}, nil
}

// 状态校验函数
func isDriverEditableStatus(status string) bool {
	switch status {
	case driverBiz.DriverStatusOnline,
		driverBiz.DriverStatusOffline,
		driverBiz.DriverStatusBusy:
		return true
	default:
		return false
	}
}

func canChangeWorkStatus(currentStatus, targetStatus string) bool {
	switch currentStatus {
	case driverBiz.DriverStatusApproved, driverBiz.DriverStatusOffline:
		return targetStatus == driverBiz.DriverStatusOnline
	case driverBiz.DriverStatusOnline:
		return targetStatus == driverBiz.DriverStatusOffline ||
			targetStatus == driverBiz.DriverStatusBusy
	case driverBiz.DriverStatusBusy:
		return targetStatus == driverBiz.DriverStatusOnline ||
			targetStatus == driverBiz.DriverStatusOffline
	default:
		return false
	}
}
