package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/omalloc/probecube-agent/internal/conf"
	"github.com/omalloc/probecube-agent/internal/engine"
	"github.com/omalloc/probecube-agent/internal/messaging"

	"github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport/http"

	_ "go.uber.org/automaxprocs"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name is the name of the compiled software.
	Name string
	// Version is the version of the compiled software.
	Version string
	// flagconf is the config flag. It accepts either a single YAML file or a
	// directory of YAML files.
	flagconf string

	id, _ = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "../../configs", "config path or directory, eg: -conf config.yaml")
}

func newApp(logger *slog.Logger, hs *http.Server, sd engine.ProbeEngineScheduler, cc *engine.Collector) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			hs,
			sd,
			cc,
		),
	)
}

func main() {
	flag.Parse()
	logger := log.NewLogger(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: false,
			Level:     slog.LevelInfo,
		}),
	).With(
		slog.String("service.id", id),
	)
	log.SetDefault(logger)

	// Configuration is read from disk at startup. It carries no secrets: the
	// node identity comes from the mTLS client certificate referenced by
	// leafnode.tls, not from an encrypted blob baked into the binary.
	c := config.New(
		config.WithSource(
			file.NewSource(flagconf),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	// Enrollment has to run before wiring: the NATS client cannot connect
	// without a client certificate, and the certificate is what enrollment
	// produces. With a certificate already on disk this is a no-op.
	if err := messaging.EnsureCertificate(context.Background(), bc.GetLeafnode(), messaging.LoadSN()); err != nil {
		panic(err)
	}

	app, cleanup, err := wireApp(&bc, logger)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	// start and wait for stop signal
	if err := app.Run(); err != nil {
		panic(err)
	}
}
