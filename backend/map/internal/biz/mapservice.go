package biz

import (
	"context"
	"fmt"
	"net/http"
	"io"
	"encoding/json"
	"errors"
)

type MapServiceBiz struct {

}

func NewMapServiceBiz() *MapServiceBiz {
	return &MapServiceBiz{}
}

// 获取驾驶信息(距离、时间)
func (msbiz *MapServiceBiz) GetDrivingInfo(ctx context.Context, origin, destination string) (string, string, error) {
	api := "https://restapi.amap.com/v3/direction/driving"
	params := fmt.Sprintf("origin=%s&destination=%s&extensions=all&output=json&key=37ebe61d0bbdc22282b36f86fe8d26e9", origin, destination)
	url := api + "?" + params
	resp, err := http.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	ddResp := &DirectionDrivingResp{}
	err = json.Unmarshal(body, ddResp)
	if err != nil {
		return "", "", err
	}
	if ddResp.Status != "1" {
		return "", "", fmt.Errorf("amap api error: %s (%s)", ddResp.Info, ddResp.Infocode)
	}
	if ddResp.Count == "0" || len(ddResp.Route.Paths) == 0 {
		return "", "", errors.New("no route found")
	}
	return ddResp.Route.Paths[0].Distance, ddResp.Route.Paths[0].Duration, nil
}

type DirectionDrivingResp struct {
	Status   string `json:"status"`
	Info     string `json:"info"`
	Infocode string `json:"infocode"`
	Count    string `json:"count"` // 高德返回字符串，如 "1"
	Route struct {
		Origin string `json:"origin"`
		Destination string `json:"destination"`
		Paths []Path `json:"paths"`
	} `json:"route"`
}

type Path struct {
	Distance string		`json:"distance"`
	Duration string		`json:"duration"`
	Strategy string		`json:"strategy"`
}

