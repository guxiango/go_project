package service

import (
	"context"
	"math/rand"

	pb "verify-code/api/verifyCode"
)

type VerifyCodeService struct {
	pb.UnimplementedVerifyCodeServer
}

func NewVerifyCodeService() *VerifyCodeService {
	return &VerifyCodeService{}
}

func (s *VerifyCodeService) GetVerifyCode(ctx context.Context, req *pb.GetVerifyCodeRequest) (*pb.GetVerifyCodeReply, error) {
    return &pb.GetVerifyCodeReply{
		Code: RandCode(int(req.Length), req.Type),
	}, nil
}

// 区分类型生成验证码
func RandCode(l int, t pb.TYPE) string {
	switch t {
		case pb.TYPE_DEFAULT:
			fallthrough
		case pb.TYPE_DIGIT:
			return randCode("0123456789", 4, l)
		case pb.TYPE_LETTER:
			return randCode("abcdefghijklmnopqrstuvwxyz", 5, l)
		case pb.TYPE_MIXED:
			return randCode("0123456789abcdefghijklmnopqrstuvwxyz", 6, l)
		default:

	}
	return ""
}

// 生成验证码，优化版
func randCode(char string, idxBits, l  int) string {
	idxMask := (1 << idxBits) - 1	// 计算索引掩码，1 << idxBits 相当于 2^idxBits，-1 相当于 2^idxBits - 1，即索引掩码
	idxMax := 63 / idxBits	// 计算索引最大值，63 / idxBits 相当于 63 / idxBits，即索引最大值
	b := make([]byte, l)
	for i , cache, remain:= 0, rand.Int63(), idxMax; i < l; i++ {
		if remain == 0 {
			cache, remain = rand.Int63(), idxMax
		}
		idx := int(cache & int64(idxMask))
		idx %= len(char)
		b[i] = char[idx]
		cache >>= idxBits
		remain--
	}
	return string(b)
}


// 生成验证码
/*
func randCode(l int, chars string) string {
	b := make([]byte, l)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))] // 随机生成字符，rand.Intn(len(chars)) 生成0到len(chars)-1的随机数，然后根据随机数索引chars中的字符，赋值给b[i]，即b[i]是chars中的一个字符
	}
	return string(b) // 返回验证码
}
*/
