package messaging

import (
	"strings"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/omalloc/probecube-agent/internal/conf"
)

type Client struct {
	*nats.Conn
	sn string
}

func NewClient(c *conf.LeafNode) (*Client, func(), error) {
	sn := loadSN()
	// 构建为 nats 连接字符串结构，多个地址用逗号分隔
	urls := strings.Join(c.Endpoint, ",")

	opts := make([]nats.Option, 0)
	opts = append(opts,
		nats.Name(sn),
		nats.ReconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Error("[NATS] Reconnect failed", "err", err)
		}),
	)

	// if bc.NatsServer.Secure.Enabled {
	// 	opts = append(opts, nats.Secure(loadTLSConfig(
	// 		bc.NatsServer.Secure.CertFile,
	// 		bc.NatsServer.Secure.KeyFile,
	// 		bc.NatsServer.Secure.CAFile)))
	// }

	if c.Nkeys != "" {
		keyPair := strings.Split(c.Nkeys, "\n")
		opts = append(opts, nats.Nkey(keyPair[1], func(b []byte) ([]byte, error) {
			sign, err := nkeys.FromSeed([]byte(keyPair[0]))
			if err != nil {
				return nil, err
			}
			return sign.Sign(b)
		}))
	}

	nc, err := nats.Connect(urls, opts...)
	if err != nil {
		return nil, func() {}, err
	}

	cleanup := func() {
		if err := nc.Drain(); err != nil {
			log.Error("[NATS] error draining connection", "err", err)
		}
	}

	return &Client{
		Conn: nc,
		sn:   sn,
	}, cleanup, nil
}

func (c *Client) SN() string {
	return c.sn
}

func (c *Client) Ready() bool {
	return c.Conn.IsConnected()
}
