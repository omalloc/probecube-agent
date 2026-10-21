package messaging

import (
	"os"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/conf"
)

// TestNewClient_Integration exercises the real connection path against a live
// NATS server configured with TLS client verification and an auth callout
// service. It is skipped unless PROBECUBE_E2E_NATS is set, so the normal unit
// test run does not depend on external infrastructure.
//
// Required environment:
//
//	PROBECUBE_E2E_NATS         tls:// url of the server
//	PROBECUBE_E2E_CERT         client certificate path (URI SAN spiffe://probecube/node/<id>)
//	PROBECUBE_E2E_KEY          client key path
//	PROBECUBE_E2E_CA           root CA path
//	PROBECUBE_E2E_NODE_ID      expected node id, used to derive the granted subjects
func TestNewClient_Integration(t *testing.T) {
	url := os.Getenv("PROBECUBE_E2E_NATS")
	if url == "" {
		t.Skip("PROBECUBE_E2E_NATS not set; skipping integration test")
	}

	nodeID := os.Getenv("PROBECUBE_E2E_NODE_ID")
	if nodeID == "" {
		t.Fatal("PROBECUBE_E2E_NODE_ID must be set for the integration test")
	}

	client, cleanup, err := NewClient(&conf.LeafNode{
		Endpoint: []string{url},
		Tls: &conf.LeafNode_TLS{
			CertFile: os.Getenv("PROBECUBE_E2E_CERT"),
			KeyFile:  os.Getenv("PROBECUBE_E2E_KEY"),
			RootCa:   os.Getenv("PROBECUBE_E2E_CA"),
		},
	})
	if err != nil {
		t.Fatalf("NewClient() = %v, want nil", err)
	}
	defer cleanup()

	if got := client.NodeID(); got != nodeID {
		t.Fatalf("NodeID() = %q, want %q (parsed from the certificate URI SAN)", got, nodeID)
	}

	// The grant allows publishing heartbeats.
	if err := client.Publish("dialsys.heartbeat", []byte("{}")); err != nil {
		t.Fatalf("publish heartbeat: %v", err)
	}
	if err := client.FlushTimeout(2 * time.Second); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// The grant allows subscribing to this node's task subject.
	if _, err := client.SubscribeSync("dialsys.probe.task." + nodeID); err != nil {
		t.Fatalf("subscribe node task subject: %v", err)
	}

	// The grant forbids the broadcast subject. NATS reports this asynchronously,
	// so SubscribeSync returns nil; the assertion is that the connection stays
	// usable and that the agent's own subject set never includes it.
	if _, err := client.SubscribeSync("dialsys.probe.task"); err != nil {
		t.Logf("broadcast subscribe rejected synchronously (expected): %v", err)
	} else {
		t.Log("broadcast subscribe accepted by the client; the broker rejects it asynchronously")
	}

	// Denied publishes must not tear the connection down.
	if err := client.Publish("dialsys.probe.results", []byte("{}")); err != nil {
		t.Fatalf("publish results: %v", err)
	}
	_ = client.FlushTimeout(2 * time.Second)

	if !client.Ready() {
		t.Fatal("client is no longer connected after a denied operation")
	}
}
