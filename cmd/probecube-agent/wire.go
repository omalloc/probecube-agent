//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"github.com/omalloc/probecube-agent/internal/conf"
	"github.com/omalloc/probecube-agent/internal/engine"
	"github.com/omalloc/probecube-agent/internal/messaging"
	"github.com/omalloc/probecube-agent/internal/server"

	"github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/google/wire"
)

// wireApp init kratos application.
func wireApp(c config.Config, bc *conf.Bootstrap) (*kratos.App, func(), error) {
	panic(wire.Build(
		wire.FieldsOf(new(*conf.Bootstrap), "Server", "Worker"),

		newLogLevelVar,
		newLogger,

		engine.ProviderSet,
		messaging.ProviderSet,
		server.ProviderSet,
		newApp,
	))
}
