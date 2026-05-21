package service

import (
	"context"

	pb "map/api/mapService"
	"map/internal/biz"
)

type MapService struct {
	pb.UnimplementedMapServiceServer
	msbiz *biz.MapServiceBiz
}

func NewMapService(msbiz *biz.MapServiceBiz) *MapService {
	return &MapService{msbiz: msbiz}
}

func (s *MapService) GetDrivingInfo(ctx context.Context, req *pb.GetDrivingInfoRequest) (*pb.GetDrivingInfoReply, error) {
	distance, duration, err := s.msbiz.GetDrivingInfo(ctx, req.Origin, req.Destination)
	if err != nil {
		return nil, err
	}
    return &pb.GetDrivingInfoReply{
		Origin: req.Origin,
		Destination: req.Destination,
		Distance: distance,
		Duration: duration,
	}, nil
}
