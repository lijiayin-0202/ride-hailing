//go:build wireinject
// +build wireinject

// 该文件只在执行 wire 生成时使用，正常编译会被 build tag 排除。
// 重新生成命令：cd cmd/ride-hailing && wire
package main

import (
	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/internal/data"
	"github.com/lijayin-0202/radish-ride-service/internal/server"
	"github.com/lijayin-0202/radish-ride-service/internal/service"
)

// wireApp init kratos application.
func wireApp(*conf.Server, *conf.Data, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		server.ProviderSet, data.ProviderSet, biz.ProviderSet, service.ProviderSet,
		wire.Bind(new(service.StationApplication), new(*biz.StationUsecase)),
		wire.Bind(new(service.ServiceAreaAdminApplication), new(*biz.ServiceAreaAdminUsecase)),
		wire.Bind(new(service.DispatchCallbackApplication), new(*biz.DispatchStateUsecase)),
		wire.Bind(new(service.DispatchApplication), new(*biz.AutoDispatchUsecase)),
		newApp,
	))
}
