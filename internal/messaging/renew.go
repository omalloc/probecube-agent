package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/nats-io/nats.go"
)

// CertificateRenewSubject is the control plane's request/reply subject for
// re-issuing a node certificate.
const CertificateRenewSubject = "dialsys.node.cert.renew"

// renewResponse mirrors the control plane's renewal payload.
type renewResponse struct {
	CertPEM  string `json:"cert_pem"`
	KeyPEM   string `json:"key_pem"`
	CAPEM    string `json:"ca_pem"`
	NotAfter int64  `json:"not_after"`
	Serial   string `json:"serial"`
	Error    string `json:"error"`
}

// Renewer keeps the client certificate fresh.
//
// Renewal happens over the already-authenticated NATS connection, which is what
// makes it safe: the control plane identifies the node by the serial number of
// the certificate that connection used, not by anything the agent asserts in the
// request body.
type Renewer struct {
	conn     *Client
	certFile string
	keyFile  string
	rootCA   string

	// renewBefore is how long before expiry a renewal is attempted.
	renewBefore time.Duration
	// retryInterval is how long to wait after a failed renewal.
	retryInterval time.Duration
}

// NewRenewer builds the renewer from the connection's TLS configuration.
func NewRenewer(conn *Client, certFile, keyFile, rootCA string) *Renewer {
	return &Renewer{
		conn:          conn,
		certFile:      certFile,
		keyFile:       keyFile,
		rootCA:        rootCA,
		renewBefore:   6 * time.Hour,
		retryInterval: 5 * time.Minute,
	}
}

// Run renews the certificate before it expires, for as long as ctx lives.
//
// It never returns on a transient failure: a node that cannot renew is still able
// to serve until the current certificate expires, so giving up early would trade
// a recoverable problem for an outage.
func (r *Renewer) Run(ctx context.Context) {
	for {
		notAfter, err := CertNotAfter(r.certFile)
		if err != nil {
			log.Error("[cert] cannot read current certificate expiry", "err", err)
			if !sleepOrDone(ctx, r.retryInterval) {
				return
			}
			continue
		}

		// Renew when the remaining lifetime drops below renewBefore, but never
		// on a schedule shorter than a minute to avoid a hot loop when the
		// certificate is already inside the window and renewal keeps failing.
		wait := time.Until(notAfter) - r.renewBefore
		if wait < time.Minute {
			wait = time.Minute
		}

		log.Info("[cert] next renewal scheduled", "cert_not_after", notAfter.Format(time.RFC3339), "in", wait.Round(time.Second))

		if !sleepOrDone(ctx, wait) {
			return
		}

		if err := r.renewOnce(ctx); err != nil {
			log.Error("[cert] renewal failed", "err", err)
			if !sleepOrDone(ctx, r.retryInterval) {
				return
			}
			continue
		}

		// The new certificate is picked up by nats.ClientCert's callback on the
		// next reconnect; forcing one now makes the change take effect at once
		// rather than at the next unrelated disconnection.
		if err := r.conn.ForceReconnect(); err != nil {
			log.Warn("[cert] reconnect after renewal failed", "err", err)
		}
	}
}

func (r *Renewer) renewOnce(ctx context.Context) error {
	serial, err := CurrentCertSerial(r.certFile)
	if err != nil {
		return fmt.Errorf("read current serial: %w", err)
	}

	msg := nats.NewMsg(CertificateRenewSubject)
	// The control plane records this serial when it issues the certificate, so
	// presenting it is proof of ownership rather than an assertion of identity.
	msg.Header.Set("cert-serial", serial)

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	reply, err := r.conn.RequestMsgWithContext(reqCtx, msg)
	if err != nil {
		return fmt.Errorf("request renewal: %w", err)
	}

	var out renewResponse
	if err := json.Unmarshal(reply.Data, &out); err != nil {
		return fmt.Errorf("decode renewal response: %w", err)
	}
	if out.Error != "" {
		return fmt.Errorf("renewal refused: %s", out.Error)
	}
	if out.CertPEM == "" || out.KeyPEM == "" {
		return fmt.Errorf("renewal returned no certificate material")
	}

	if err := writeSecret(r.certFile, []byte(out.CertPEM)); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	if err := writeSecret(r.keyFile, []byte(out.KeyPEM)); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if out.CAPEM != "" && !fileExists(r.rootCA) {
		if err := writeSecret(r.rootCA, []byte(out.CAPEM)); err != nil {
			return fmt.Errorf("write root ca: %w", err)
		}
	}

	log.Info("[cert] renewed", "new_serial", out.Serial,
		"not_after", time.Unix(out.NotAfter, 0).Format(time.RFC3339))
	return nil
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
