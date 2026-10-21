package messaging

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"

	"github.com/go-kratos/kratos/v3/log"
)

// nodeIdentityURIPrefix is the URI SAN prefix the centre's auth callout service
// uses to derive a node's identity. The agent parses it out of its own client
// certificate so that request/reply inboxes stay inside the subject space the
// callout service actually grants.
const nodeIdentityURIPrefix = "spiffe://probecube/node/"

// loadNodeID reads the node UUID out of the client certificate's URI SAN.
//
// The certificate is the source of truth for identity: the auth callout service
// derives permissions from the chain the server validated during the TLS
// handshake, so the agent must never treat a self-reported value as authority.
// A missing URI SAN is not fatal here; the caller falls back to the serial
// number, which keeps older certificates working.
func loadNodeID(certFile string) string {
	if certFile == "" {
		return ""
	}

	raw, err := os.ReadFile(certFile)
	if err != nil {
		log.Warn("[NATS] unable to read client certificate for node identity", "cert_file", certFile, "err", err)
		return ""
	}

	for {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			return ""
		}
		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			log.Warn("[NATS] unable to parse client certificate", "err", err)
			return ""
		}

		for _, uri := range cert.URIs {
			if value := uri.String(); strings.HasPrefix(value, nodeIdentityURIPrefix) {
				return strings.TrimPrefix(value, nodeIdentityURIPrefix)
			}
		}
		return ""
	}
}

// inboxPrefix derives the request/reply inbox prefix so replies stay inside the
// subject space the auth callout grant allows.
//
// The serial number is used, not the certificate identity: the callout builds
// the node's permission set from the SN (it is the routing key used for task
// dispatch), so granting "_INBOX.<uuid>.>" while the agent listens on
// "_INBOX.<sn>.>" would make every request/reply silently time out.
func inboxPrefix(sn, nodeID string) string {
	identity := sn
	if identity == "" {
		identity = nodeID
	}
	if identity == "" {
		return ""
	}
	// CustomInboxPrefix rejects a trailing dot; the library appends "." itself
	// before the generated nuid.
	return "_INBOX." + identity
}
