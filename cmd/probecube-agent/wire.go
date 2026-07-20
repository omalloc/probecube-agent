//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"log/slog"

	"github.com/omalloc/probecube-agent/internal/conf"
	"github.com/omalloc/probecube-agent/internal/engine"
	"github.com/omalloc/probecube-agent/internal/messaging"
	"github.com/omalloc/probecube-agent/internal/server"

	"github.com/go-kratos/kratos/v3"
	"github.com/google/wire"
)

// wireApp init kratos application.
func wireApp(*conf.Bootstrap, *slog.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		wire.FieldsOf(new(*conf.Bootstrap), "Server", "LeafNode", "Worker"),

		engine.ProviderSet,
		messaging.ProviderSet,
		server.ProviderSet,
		newApp,
	))
}
