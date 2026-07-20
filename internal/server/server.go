package server

import (
	"github.com/google/wire"
	"github.com/omalloc/probecube-agent/internal/conf"
)

// ProviderSet is server providers.
var ProviderSet = wire.NewSet(
	ProviderWorkerConfig,

	NewHTTPServer,
	NewSchedulerServer,
)

func ProviderWorkerConfig(c *conf.Worker) SchedulerConfig {
	return c
}
