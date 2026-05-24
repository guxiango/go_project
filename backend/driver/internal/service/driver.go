package service

import (
	"context"
	"database/sql"
	orderAPI "driver/api/order"
	"regexp"
	"strconv"
	"strings"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"google.golang.org/grpc/metadata"
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

// Register creates a driver account after verification-code validation.
func (s *DriverService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterReply, error) {
	if ok, message := s.validatePhoneAndVerifyCode(ctx, req.Telephone, req.VerifyCode); !ok {
		return &pb.RegisterReply{
			Code:    1,
			Message: message,
		}, nil
	}
	// Create the driver only when the phone number has not been registered.
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

// Login authenticates a driver and issues a JWT.
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
	// Generate a token and persist it so later requests can be checked.
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

// validatePhoneAndVerifyCode checks phone format and verification-code correctness.
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

// isValidPhoneNumber checks mainland China mobile phone format.
func isValidPhoneNumber(phone string) bool {
	return phoneNumberPattern.MatchString(phone)
}

// UpdateDriverProfile updates the current driver's profile.
func (s *DriverService) UpdateDriverProfile(ctx context.Context, req *pb.UpdateDriverProfileRequest) (*pb.UpdateDriverProfileReply, error) {
	driverId, ok := getDriverIDFromContext(ctx)
	if !ok {
		return &pb.UpdateDriverProfileReply{
			Code:    1,
			Message: "未授权，请先登录",
		}, nil
	}
	// Update only the profile owned by the current driver.
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

// UpdateWorkStatus updates the current driver's work status.
func (s *DriverService) UpdateWorkStatus(ctx context.Context, req *pb.UpdateWorkStatusRequest) (*pb.UpdateWorkStatusReply, error) {
	driverId, ok := getDriverIDFromContext(ctx)
	if !ok {
		return nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_UPDATE_WORK_STATUS_REQUEST", "request is required")
	}
	targetStatus := strings.TrimSpace(req.Status)
	if !isDriverEditableStatus(targetStatus) {
		return nil, kerrors.BadRequest("INVALID_DRIVER_STATUS", "only online or offline can be set manually")
	}
	driver, err := s.DriverData.GetDriverByID(ctx, driverId)
	if err != nil {
		return nil, kerrors.InternalServer("GET_DRIVER_FAILED", "get driver failed")
	}
	currentStatus := ""
	if driver.Status.Valid {
		currentStatus = driver.Status.String
	}
	if !canChangeWorkStatus(currentStatus, targetStatus) {
		return nil, kerrors.Conflict("DRIVER_STATUS_TRANSITION_NOT_ALLOWED", "current status does not allow this transition")
	}
	if err := s.DriverData.UpdateDriverStatusByID(ctx, driverId, currentStatus, targetStatus); err != nil {
		return nil, kerrors.Conflict("DRIVER_STATUS_CHANGED", "driver status changed")
	}
	return &pb.UpdateWorkStatusReply{
		Code:    0,
		Message: "update status success",
		Status:  targetStatus,
	}, nil
}

// isDriverEditableStatus checks statuses that drivers may set themselves.
func isDriverEditableStatus(status string) bool {
	switch status {
	case driverBiz.DriverStatusOnline,
		driverBiz.DriverStatusOffline:
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
		return targetStatus == driverBiz.DriverStatusOffline
	default:
		return false
	}
}

// InternalAuditDriverProfile lets admins audit driver profile submissions.
func (s *DriverService) InternalAuditDriverProfile(ctx context.Context, req *pb.InternalAuditDriverProfileRequest) (*pb.InternalAuditDriverProfileReply, error) {
	if req.GetDriverId() == 0 || req.GetAdminId() == 0 {
		return &pb.InternalAuditDriverProfileReply{
			Code:    1,
			Message: "参数错误",
		}, nil
	}

	driver, err := s.DriverData.GetDriverByID(ctx, uint(req.GetDriverId()))
	if err != nil {
		return &pb.InternalAuditDriverProfileReply{
			Code:    1,
			Message: "获取司机信息失败",
		}, nil
	}

	currentStatus := ""
	if driver.Status.Valid {
		currentStatus = driver.Status.String
	}
	if currentStatus != driverBiz.DriverStatusPending {
		return &pb.InternalAuditDriverProfileReply{
			Code:    1,
			Message: "当前司机状态不允许审核",
			Status:  currentStatus,
		}, nil
	}

	targetStatus := driverBiz.DriverStatusStop
	if req.GetApproved() {
		targetStatus = driverBiz.DriverStatusApproved
	} else if strings.TrimSpace(req.GetReason()) == "" {
		return &pb.InternalAuditDriverProfileReply{
			Code:    1,
			Message: "审核拒绝时原因不能为空",
			Status:  currentStatus,
		}, nil
	}

	if err := s.DriverData.AuditDriverStatusByID(ctx, uint(req.GetDriverId()), currentStatus, targetStatus); err != nil {
		return &pb.InternalAuditDriverProfileReply{
			Code:    1,
			Message: "审核司机信息失败",
			Status:  currentStatus,
		}, nil
	}

	return &pb.InternalAuditDriverProfileReply{
		Code:    0,
		Message: "审核司机信息成功",
		Status:  targetStatus,
	}, nil
}

// InternalListPendingDrivers returns driver profiles waiting for admin review.
func (s *DriverService) InternalListPendingDrivers(ctx context.Context, req *pb.InternalListPendingDriversRequest) (*pb.InternalListPendingDriversReply, error) {
	page := req.GetPage()
	if page <= 0 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	drivers, total, err := s.DriverData.ListDriversByStatus(ctx, driverBiz.DriverStatusPending, int(offset), int(pageSize))
	if err != nil {
		return &pb.InternalListPendingDriversReply{
			Code:    1,
			Message: "获取司机信息失败",
		}, nil
	}
	var msg []*pb.InternalPendingDriver
	for _, driver := range drivers {
		msg = append(msg, &pb.InternalPendingDriver{
			DriverId:      uint64(driver.ID),
			Name:          driver.Name.String,
			Telephone:     driver.Telephone,
			Status:        driver.Status.String,
			IdNumber:      driver.IdNumber.String,
			IdImageA:      driver.IdImageA.String,
			IdImageB:      driver.IdImageB.String,
			LicenseImageA: driver.LicenseImageA.String,
			LicenseImageB: driver.LicenseImageB.String,
			DistinctCode:  driver.DistinctCode.String,
			UpdatedAt:     driver.UpdatedAt.Unix(),
		})
	}
	return &pb.InternalListPendingDriversReply{
		Code:    0,
		Message: "获取司机信息成功",
		Total:   total,
		Drivers: msg,
	}, nil
}

// AcceptOrder lets the current driver accept a pending order.
func (s *DriverService) AcceptOrder(ctx context.Context, req *pb.AcceptOrderRequest) (*pb.AcceptOrderReply, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_ACCEPT_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	driverID, ok := getDriverIDFromContext(ctx)
	if !ok {
		return nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}

	previousStatus, err := s.reserveDriverForOrder(ctx, driverID)
	if err != nil {
		return nil, err
	}
	orderCtx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		_ = s.restoreDriverStatus(ctx, driverID, previousStatus)
		return nil, err
	}
	reply, err := orderClient.AcceptOrder(orderCtx, &orderAPI.AcceptOrderRequest{OrderId: req.OrderId})
	if err != nil {
		_ = s.restoreDriverStatus(ctx, driverID, previousStatus)
		return nil, err
	}
	return convertAcceptOrderReply(reply), nil
}

func (s *DriverService) authenticatedOrderClient(ctx context.Context) (context.Context, orderAPI.OrderClient, error) {
	driverID, ok := getDriverIDFromContext(ctx)
	if !ok {
		return nil, nil, kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}
	orderClient, err := s.DriverBiz.OrderClient()
	if err != nil {
		return nil, nil, kerrors.InternalServer("ORDER_CLIENT_UNAVAILABLE", "order client unavailable")
	}
	ctx = metadata.AppendToOutgoingContext(
		ctx,
		"x-user-id", strconv.FormatUint(uint64(driverID), 10),
		"x-user-role", "driver",
	)
	return ctx, orderClient, nil
}

func (s *DriverService) reserveDriverForOrder(ctx context.Context, driverID uint) (string, error) {
	previousStatus, err := s.DriverData.UpdateDriverStatusFromAny(ctx, driverID, []string{
		driverBiz.DriverStatusApproved,
		driverBiz.DriverStatusOnline,
	}, driverBiz.DriverStatusBusy)
	if err != nil {
		return "", kerrors.Conflict("DRIVER_NOT_AVAILABLE", "driver must be approved or online before accepting an order")
	}
	return previousStatus, nil
}

func (s *DriverService) restoreDriverStatus(ctx context.Context, driverID uint, previousStatus string) error {
	if previousStatus == "" {
		return nil
	}
	return s.DriverData.UpdateDriverStatusByID(ctx, driverID, driverBiz.DriverStatusBusy, previousStatus)
}

func (s *DriverService) releaseDriverAfterOrder(ctx context.Context) error {
	driverID, ok := getDriverIDFromContext(ctx)
	if !ok {
		return kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}
	if err := s.DriverData.UpdateDriverStatusByID(ctx, driverID, driverBiz.DriverStatusBusy, driverBiz.DriverStatusOnline); err != nil {
		return kerrors.InternalServer("DRIVER_STATUS_RELEASE_FAILED", "order completed but driver status release failed")
	}
	return nil
}

func (s *DriverService) ensureDriverBusy(ctx context.Context) error {
	driverID, ok := getDriverIDFromContext(ctx)
	if !ok {
		return kerrors.Unauthorized("UNAUTHORIZED", "please login first")
	}
	driver, err := s.DriverData.GetDriverByID(ctx, driverID)
	if err != nil {
		return kerrors.InternalServer("GET_DRIVER_FAILED", "get driver failed")
	}
	if !driver.Status.Valid || driver.Status.String != driverBiz.DriverStatusBusy {
		return kerrors.Conflict("DRIVER_NOT_BUSY", "driver must be busy on an accepted order")
	}
	return nil
}

// StartOrder lets the current driver start an accepted order.
func (s *DriverService) StartOrder(ctx context.Context, req *pb.StartOrderRequest) (*pb.StartOrderReply, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_START_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	if err := s.ensureDriverBusy(ctx); err != nil {
		return nil, err
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.StartOrder(ctx, &orderAPI.StartOrderRequest{OrderId: req.OrderId})
	if err != nil {
		return nil, err
	}
	return convertStartOrderReply(reply), nil
}

// FinishOrder lets the current driver finish a started order.
func (s *DriverService) FinishOrder(ctx context.Context, req *pb.FinishOrderRequest) (*pb.FinishOrderReply, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_FINISH_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	if err := s.ensureDriverBusy(ctx); err != nil {
		return nil, err
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.FinishOrder(ctx, &orderAPI.FinishOrderRequest{OrderId: req.OrderId})
	if err != nil {
		return nil, err
	}
	if err := s.releaseDriverAfterOrder(ctx); err != nil {
		return nil, err
	}
	return convertFinishOrderReply(reply), nil
}

// CancelOrder lets the current driver cancel an accepted order.
func (s *DriverService) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderReply, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_CANCEL_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	if err := s.ensureDriverBusy(ctx); err != nil {
		return nil, err
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.CancelOrder(ctx, &orderAPI.CancelOrderRequest{
		OrderId: req.OrderId,
		Reason:  strings.TrimSpace(req.Reason),
	})
	if err != nil {
		return nil, err
	}
	if err := s.releaseDriverAfterOrder(ctx); err != nil {
		return nil, err
	}
	return convertCancelOrderReply(reply), nil
}

// GetOrder returns one order visible to the current driver.
func (s *DriverService) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderReply, error) {
	if req == nil {
		return nil, kerrors.BadRequest("INVALID_GET_ORDER_REQUEST", "request is required")
	}
	if req.OrderId == 0 {
		return nil, kerrors.BadRequest("INVALID_ORDER_ID", "order_id is required")
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.GetOrder(ctx, &orderAPI.GetOrderRequest{OrderId: req.OrderId})
	if err != nil {
		return nil, err
	}
	return convertGetOrderReply(reply), nil
}

// ListPendingOrders returns orders available for drivers to accept.
func (s *DriverService) ListPendingOrders(ctx context.Context, req *pb.ListPendingOrdersRequest) (*pb.ListPendingOrdersReply, error) {
	if req == nil {
		req = &pb.ListPendingOrdersRequest{}
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.ListPendingOrders(ctx, &orderAPI.ListPendingOrdersRequest{
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	return convertListPendingOrdersReply(reply), nil
}

// ListDriverOrders returns orders assigned to the current driver.
func (s *DriverService) ListDriverOrders(ctx context.Context, req *pb.ListDriverOrdersRequest) (*pb.ListDriverOrdersReply, error) {
	if req == nil {
		req = &pb.ListDriverOrdersRequest{}
	}
	ctx, orderClient, err := s.authenticatedOrderClient(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := orderClient.ListDriverOrders(ctx, &orderAPI.ListDriverOrdersRequest{
		Status:   strings.TrimSpace(req.Status),
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	return convertListDriverOrdersReply(reply), nil
}

func convertAcceptOrderReply(reply *orderAPI.AcceptOrderReply) *pb.AcceptOrderReply {
	if reply == nil {
		return nil
	}
	return &pb.AcceptOrderReply{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}
}

func convertStartOrderReply(reply *orderAPI.StartOrderReply) *pb.StartOrderReply {
	if reply == nil {
		return nil
	}
	return &pb.StartOrderReply{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}
}

func convertFinishOrderReply(reply *orderAPI.FinishOrderReply) *pb.FinishOrderReply {
	if reply == nil {
		return nil
	}
	return &pb.FinishOrderReply{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}
}

func convertCancelOrderReply(reply *orderAPI.CancelOrderReply) *pb.CancelOrderReply {
	if reply == nil {
		return nil
	}
	return &pb.CancelOrderReply{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}
}

func convertGetOrderReply(reply *orderAPI.GetOrderReply) *pb.GetOrderReply {
	if reply == nil {
		return nil
	}
	return &pb.GetOrderReply{
		Code:    reply.Code,
		Message: reply.Message,
		Order:   convertOrderInfo(reply.Order),
	}
}

func convertListPendingOrdersReply(reply *orderAPI.ListPendingOrdersReply) *pb.ListPendingOrdersReply {
	if reply == nil {
		return nil
	}
	return &pb.ListPendingOrdersReply{
		Code:    reply.Code,
		Message: reply.Message,
		Orders:  convertOrderInfos(reply.Orders),
		Total:   reply.Total,
	}
}

func convertListDriverOrdersReply(reply *orderAPI.ListDriverOrdersReply) *pb.ListDriverOrdersReply {
	if reply == nil {
		return nil
	}
	return &pb.ListDriverOrdersReply{
		Code:    reply.Code,
		Message: reply.Message,
		Orders:  convertOrderInfos(reply.Orders),
		Total:   reply.Total,
	}
}

func convertOrderInfos(orders []*orderAPI.OrderInfo) []*pb.OrderInfo {
	infos := make([]*pb.OrderInfo, 0, len(orders))
	for _, order := range orders {
		infos = append(infos, convertOrderInfo(order))
	}
	return infos
}

func convertOrderInfo(order *orderAPI.OrderInfo) *pb.OrderInfo {
	if order == nil {
		return nil
	}
	return &pb.OrderInfo{
		OrderId:       order.OrderId,
		OrderNo:       order.OrderNo,
		CustomerId:    order.CustomerId,
		DriverId:      order.DriverId,
		Origin:        order.Origin,
		Destination:   order.Destination,
		Distance:      order.Distance,
		Duration:      order.Duration,
		EstimatePrice: order.EstimatePrice,
		Status:        order.Status,
		CreatedAt:     order.CreatedAt,
		AcceptedAt:    order.AcceptedAt,
		StartedAt:     order.StartedAt,
		FinishedAt:    order.FinishedAt,
		CancelledAt:   order.CancelledAt,
	}
}
