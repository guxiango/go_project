package registry

import (
	"errors"

	"order/internal/conf"

	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/google/wire"
	"github.com/hashicorp/consul/api"
)

var ProviderSet = wire.NewSet(NewConsulClient, NewConsulRegistry)

func NewConsulClient(svc *conf.Service) (*api.Client, error) {
	if svc == nil || svc.Consul == nil || svc.Consul.Addr == "" {
		return nil, errors.New("consul address is required")
	}
	cfg := api.DefaultConfig()
	cfg.Address = svc.Consul.Addr
	return api.NewClient(cfg)
}

func NewConsulRegistry(client *api.Client) (*consul.Registry, error) {
	if client == nil {
		return nil, errors.New("consul client is required")
	}
	return consul.New(client), nil
}
