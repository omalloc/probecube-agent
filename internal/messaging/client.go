package messaging

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/nats-io/nats.go"
	"github.com/omalloc/probecube-agent/internal/conf"
)

// permissionSubjectRe extracts the offending subject from a NATS permission
// violation message. It mirrors the pattern the client library itself uses to
// attribute violations to subscriptions.
var permissionSubjectRe = regexp.MustCompile(`(?:Subscription to|Publish to) "(\S+)"`)

type Client struct {
	*nats.Conn
	sn string
	// nodeID is the identity carried by the client certificate's URI SAN. It is
	// empty when the certificate does not carry one.
	nodeID string
}

// NewClient dials the centre NATS server over mTLS. There is no NKey seed or
// credentials file: the node proves who it is with its client certificate and
// the server's auth callout service decides what it may publish and subscribe.
func NewClient(c *conf.LeafNode) (*Client, func(), error) {
	if err := validateLeafNode(c); err != nil {
		return nil, func() {}, err
	}

	tlsCfg := c.GetTls()
	sn := LoadSN()
	nodeID := loadNodeID(tlsCfg.GetCertFile())

	// Multiple addresses are joined into a single comma-separated URL list.
	urls := strings.Join(c.GetEndpoint(), ",")

	opts := []nats.Option{
		nats.Name(sn),
		nats.RootCAs(tlsCfg.GetRootCa()),
		// ClientCert installs a TLSCertCB, so the certificate is re-read from
		// disk on every (re)connect. Rotating the files therefore takes effect
		// without restarting the process.
		nats.ClientCert(tlsCfg.GetCertFile(), tlsCfg.GetKeyFile()),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		// The server issues a short-lived user JWT per connection. When it
		// expires the library reconnects and triggers a fresh auth callout. The
		// default behaviour aborts after seeing the same auth error twice, which
		// would leave a node permanently stranded after a brief callout outage.
		nats.IgnoreAuthErrorAbort(),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Error("[NATS] disconnected", "err", err)
			}
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) {
			log.Info("[NATS] reconnected", "url", conn.ConnectedUrl())
		}),
		nats.ErrorHandler(natsErrorHandler),
	}

	if prefix := inboxPrefix(sn, nodeID); prefix != "" {
		opts = append(opts, nats.CustomInboxPrefix(prefix))
	}

	if c.GetNkeys() != "" {
		log.Warn("[NATS] leafnode.nkeys is deprecated and ignored; the agent authenticates with its mTLS client certificate")
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
		Conn:   nc,
		sn:     sn,
		nodeID: nodeID,
	}, cleanup, nil
}

// validateLeafNode enforces that the agent cannot fall back to a plaintext or
// anonymous connection. A misconfigured deployment must fail at startup rather
// than silently run without authentication.
func validateLeafNode(c *conf.LeafNode) error {
	if c == nil {
		return errors.New("leafnode configuration is required")
	}
	if len(c.GetEndpoint()) == 0 {
		return errors.New("leafnode.endpoint must contain at least one NATS URL")
	}
	tlsCfg := c.GetTls()
	if tlsCfg == nil {
		return errors.New("leafnode.tls is required: the agent authenticates with an mTLS client certificate")
	}
	for _, field := range []struct{ name, value string }{
		{"leafnode.tls.cert_file", tlsCfg.GetCertFile()},
		{"leafnode.tls.key_file", tlsCfg.GetKeyFile()},
		{"leafnode.tls.root_ca", tlsCfg.GetRootCa()},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
		if _, err := os.Stat(field.value); err != nil {
			return fmt.Errorf("%s: %w", field.name, err)
		}
	}
	return nil
}

// natsErrorHandler surfaces asynchronous protocol errors. Permission violations
// matter most: the library reports them out-of-band, so without this hook a node
// whose granted subject set does not match what the agent subscribes to would
// look healthy while silently receiving no tasks.
func natsErrorHandler(_ *nats.Conn, sub *nats.Subscription, err error) {
	if !errors.Is(err, nats.ErrPermissionViolation) {
		log.Error("[NATS] async error", "err", err)
		return
	}

	// The library invokes the async error callback with a nil subscription, so
	// the subject has to come from the error text, which is the only place it
	// survives. Without this the log entry is useless for diagnosing a contract
	// mismatch.
	subject := "<unknown>"
	if sub != nil && sub.Subject != "" {
		subject = sub.Subject
	} else if m := permissionSubjectRe.FindStringSubmatch(err.Error()); len(m) == 2 {
		subject = m[1]
	}

	log.Error("[NATS] PERMISSION VIOLATION — subject contract mismatch between the agent and the auth callout grant",
		"subject", subject, "err", err)
}

func (c *Client) SN() string {
	return c.sn
}

// NodeID returns the identity carried by the client certificate. It falls back
// to the serial number when the certificate has no URI SAN.
func (c *Client) NodeID() string {
	if c.nodeID != "" {
		return c.nodeID
	}
	return c.sn
}

func (c *Client) Ready() bool {
	return c.Conn.IsConnected()
}
