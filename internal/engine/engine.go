package engine

import (
	"context"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(NewCollector)

type ProbeEngineScheduler interface {
	transport.Server

	Submit(ctx context.Context, task *Task)
	Results() <-chan *Result
}
