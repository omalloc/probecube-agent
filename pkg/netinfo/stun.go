// Package netinfo discovers the agent's public and local network addresses.
package netinfo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/pion/stun/v3"
)

var defaultSTUNServers = []string{
	"stun.miwifi.com:3478",
	"stun.cloudflare.com:3478",
}

const stunServerTimeout = 2 * time.Second

type Addrs struct {
	PublicIP string
	LocalIP  string
}

// Discover queries the default STUN servers in order until one succeeds.
func Discover(ctx context.Context) (Addrs, error) {
	return Fetch(ctx, defaultSTUNServers)
}

// Fetch queries the supplied STUN servers in order until one succeeds.
func Fetch(ctx context.Context, servers []string) (Addrs, error) {
	if len(servers) == 0 {
		return Addrs{}, fmt.Errorf("fetch network addresses: no STUN servers configured")
	}

	serverErrors := make([]error, 0, len(servers))
	for _, server := range servers {
		// Each server gets a complete attempt. An upstream deadline must not
		// prevent the remaining servers from being tried.
		attemptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stunServerTimeout)
		addrs, err := fetch(attemptCtx, server)
		cancel()

		if err == nil {
			return addrs, nil
		}
		serverErrors = append(serverErrors, fmt.Errorf("%s: %w", server, err))
	}

	return Addrs{}, fmt.Errorf("fetch network addresses from STUN servers: %w", errors.Join(serverErrors...))
}

func fetch(ctx context.Context, server string) (Addrs, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", server)
	if err != nil {
		return Addrs{}, fmt.Errorf("dial STUN server: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return Addrs{}, fmt.Errorf("set STUN deadline: %w", err)
		}
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	defer stopCancel()

	localIP := ipFromAddr(conn.LocalAddr())
	if localIP == "" {
		_ = conn.Close()
		return Addrs{}, fmt.Errorf("determine local IP: connection has no IP address")
	}

	client, err := stun.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return Addrs{}, fmt.Errorf("create STUN client: %w", err)
	}
	defer client.Close()

	var mappedAddr stun.XORMappedAddress
	var responseErr error
	err = client.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), func(event stun.Event) {
		if event.Error != nil {
			responseErr = event.Error
			return
		}
		responseErr = mappedAddr.GetFrom(event.Message)
	})
	if err != nil {
		return Addrs{}, fmt.Errorf("send STUN binding request: %w", err)
	}
	if responseErr != nil {
		return Addrs{}, fmt.Errorf("receive STUN binding response: %w", responseErr)
	}
	if mappedAddr.IP == nil {
		return Addrs{}, fmt.Errorf("receive STUN binding response: missing public IP")
	}

	return Addrs{PublicIP: mappedAddr.IP.String(), LocalIP: localIP}, nil
}

func ipFromAddr(addr net.Addr) string {
	switch addr := addr.(type) {
	case *net.TCPAddr:
		if addr.IP == nil {
			return ""
		}
		return addr.IP.String()
	case *net.UDPAddr:
		if addr.IP == nil {
			return ""
		}
		return addr.IP.String()
	default:
		if addr == nil {
			return ""
		}
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return ""
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return ""
		}
		return ip.String()
	}
}
