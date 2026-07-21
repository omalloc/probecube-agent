package netinfo

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestFetch_ReturnsPublicAndConnectionLocalIP(t *testing.T) {
	server := newSTUNServer(t, "203.0.113.10")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := Fetch(ctx, []string{server.LocalAddr().String()})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.PublicIP != "203.0.113.10" {
		t.Errorf("public IP = %q, want %q", got.PublicIP, "203.0.113.10")
	}
	if got.LocalIP != "127.0.0.1" {
		t.Errorf("local IP = %q, want %q", got.LocalIP, "127.0.0.1")
	}
}

func TestFetch_RetriesAfterFailure(t *testing.T) {
	server := newSTUNServer(t, "203.0.113.20")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := Fetch(ctx, []string{"invalid STUN address", server.LocalAddr().String()})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.PublicIP != "203.0.113.20" {
		t.Errorf("public IP = %q, want %q", got.PublicIP, "203.0.113.20")
	}
}

func TestFetch_GivesEachServerItsOwnTimeout(t *testing.T) {
	server := newDelayedSTUNServer(t, "203.0.113.30", 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	got, err := Fetch(ctx, []string{server.LocalAddr().String()})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.PublicIP != "203.0.113.30" {
		t.Errorf("public IP = %q, want %q", got.PublicIP, "203.0.113.30")
	}
}

func TestFetch_FailsAfterTryingAllServers(t *testing.T) {
	servers := []string{"invalid STUN address one", "invalid STUN address two"}

	_, err := Fetch(context.Background(), servers)
	if err == nil {
		t.Fatal("Fetch() error = nil, want errors from all STUN servers")
	}
	for _, server := range servers {
		if !strings.Contains(err.Error(), server) {
			t.Errorf("Fetch() error = %q, want server %q", err, server)
		}
	}
}

func TestFetch_RejectsEmptyServerList(t *testing.T) {
	if _, err := Fetch(context.Background(), nil); err == nil {
		t.Fatal("Fetch() error = nil, want an error")
	}
}

func newSTUNServer(t *testing.T, publicIP string) *net.UDPConn {
	return newDelayedSTUNServer(t, publicIP, 0)
}

func newDelayedSTUNServer(t *testing.T, publicIP string, delay time.Duration) *net.UDPConn {
	t.Helper()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	go func() {
		buffer := make([]byte, 1500)
		n, clientAddr, err := server.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := &stun.Message{Raw: append([]byte(nil), buffer[:n]...)}
		if request.Decode() != nil {
			return
		}
		time.Sleep(delay)
		mappedAddr := stun.XORMappedAddress{IP: net.ParseIP(publicIP), Port: clientAddr.Port}
		response, err := stun.Build(
			stun.NewTransactionIDSetter(request.TransactionID),
			stun.BindingSuccess,
			mappedAddr,
		)
		if err == nil {
			_, _ = server.WriteToUDP(response.Raw, clientAddr)
		}
	}()

	return server
}
