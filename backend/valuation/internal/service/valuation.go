package service

import (
	"context"
	"valuation/internal/biz"
	pb "valuation/api/valuation"
)

type ValuationService struct {
	pb.UnimplementedValuationServer
	// 业务逻辑
	vb *biz.ValuationBiz
}

func NewValuationService(vb *biz.ValuationBiz) *ValuationService {
	return &ValuationService{vb: vb}
}

func (s *ValuationService) GetEstimatePrice(ctx context.Context, req *pb.GetEstimatePriceRequest) (*pb.GetEstimatePriceReply, error) {
	distance, duration, err := s.vb.GetDrivingInfo(ctx, req.Origin, req.Destination)
	if err != nil {
		return nil, err
	}
	price, err := s.vb.GetPrice(ctx, distance, duration, 1, 10)
	if err != nil {
		return nil, err
	}
    return &pb.GetEstimatePriceReply{
		Origin: req.Origin,
		Destination: req.Destination,
		Price: price,
	}, nil
}
