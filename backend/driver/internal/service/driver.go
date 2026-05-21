package service

import (
	"context"
	"regexp"
	"time"

	pb "driver/api/driver"
	verifyCode "driver/api/verifyCode"
	driverBiz "driver/internal/biz"
	driverData "driver/internal/data"
)

type DriverService struct {
	pb.UnimplementedDriverServer
	driverData *driverData.DriverData
	driverBiz *driverBiz.DriverBiz
}

func NewDriverService(driverData *driverData.DriverData, driverBiz *driverBiz.DriverBiz) *DriverService {
	return &DriverService{driverData: driverData, driverBiz: driverBiz}
}

func (s *DriverService) GetVerifyCode(ctx context.Context, req *pb.GetVerifyCodeRequest) (*pb.GetVerifyCodeReply, error) {
	pattern := `^1[3-9]\d{9}$`
	match, err := regexp.MatchString(pattern, req.Telephone)
	if err != nil {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "校验手机号时发生异常，请稍后重试",
		}, nil
	}
	if !match {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "手机号格式不正确，请输入11位中国大陆手机号",
		}, nil
	}

	reply, err := s.driverBiz.GetVerifyCode(ctx, req.Telephone, 6, verifyCode.TYPE_DEFAULT)
	if err != nil {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "获取验证码失败，请稍后重试",
		}, nil
	}

	const lifetime = 60
	err = s.driverData.SetVerifyCode(ctx, req.Telephone, reply.Code, lifetime)
	if err != nil {
		return &pb.GetVerifyCodeReply{
			Code:    1,
			Message: "验证码暂存失败（缓存不可用），请稍后重试",
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

// 提交手机号
func (s *DriverService) SubmitPhone(ctx context.Context, req *pb.SubmitPhoneRequest) (*pb.SubmitPhoneReply, error) {
	// 校验验证码 (略)
	// 司机是否注册的校验 (略)
	// 司机是否在黑名单中的校验 (略)

	// 将司机入库,并设置状态为stop暂时停用
	
	return &pb.SubmitPhoneReply{
		Code:    0,
		Message: "提交成功",
		Status:  "stop",
	}, nil
}