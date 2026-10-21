package messaging

import (
	"os"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/conf"
)

// TestNewClient_Integration_Renewal verifies that when the short-lived user JWT
// expires, the library reconnects and a fresh auth callout is performed, so the
// node keeps working without operator intervention.
//
// Run with a callout service configured to issue a TTL shorter than
// PROBECUBE_E2E_TTL_SECONDS.
func TestNewClient_Integration_Renewal(t *testing.T) {
	url := os.Getenv("PROBECUBE_E2E_NATS")
	if url == "" {
		t.Skip("PROBECUBE_E2E_NATS not set; skipping integration test")
	}
	nodeID := os.Getenv("PROBECUBE_E2E_NODE_ID")
	if nodeID == "" {
		t.Fatal("PROBECUBE_E2E_NODE_ID must be set")
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

	if !client.Ready() {
		t.Fatal("not connected after dial")
	}

	// Wait past the token TTL. The server disconnects with "User Authentication
	// Expired" and the client must reconnect and trigger a new callout.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if client.Ready() && client.Stats().Reconnects > 0 {
			t.Logf("reconnected after token expiry (reconnects=%d)", client.Stats().Reconnects)
			// The new connection must still be authorised.
			if _, err := client.SubscribeSync("dialsys.probe.task." + nodeID); err != nil {
				t.Fatalf("subscribe after renewal: %v", err)
			}
			return
		}
	}
	t.Fatalf("no reconnection observed within the deadline (reconnects=%d, status=%v)",
		client.Stats().Reconnects, client.Status())
}
