package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// ErrRefused is a connection or URL the guard refuses: a scheme other than http or https, credentials in the URL, a
// port other than 80 or 443, or a host with no address the allowlist accepts.
var ErrRefused = errors.New("refused by the link checker's guard")

// allowedPorts are the only ports a link may reach.
var allowedPorts = map[uint16]bool{80: true, 443: true}

// connectTimeout bounds one connection attempt.
const connectTimeout = 5 * time.Second

// Resolver looks up a host name's addresses, as *net.Resolver does.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// DialFunc connects to an address given as an IP and port, as (*net.Dialer).DialContext does.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// guard connects to a host's addresses that PublicAddress accepts, on an allowed port, and to nothing else. It
// resolves the host itself and dials the address it checked, never the name, so a name that resolves to another
// address on a second lookup (DNS rebinding) cannot reach it.
type guard struct {
	resolver Resolver
	dial     DialFunc
}

// dialContext is the transport's DialContext: address is the URL's host and port.
func (g guard) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := splitAllowedHostPort(address)
	if err != nil {
		return nil, err
	}

	var addrs []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{literal}
	} else if addrs, err = g.resolver.LookupNetIP(ctx, "ip", host); err != nil {
		return nil, err
	}

	var lastErr error
	for _, addr := range addrs {
		if !PublicAddress(addr) {
			continue
		}
		conn, err := g.dial(ctx, network, netip.AddrPortFrom(addr.WithZone("").Unmap(), port).String())
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return nil, fmt.Errorf("%w: the host has no public address", ErrRefused)
	}

	return nil, lastErr
}

// splitAllowedHostPort splits address into its host and port, refusing a port that is not allowed.
func splitAllowedHostPort(address string) (string, uint16, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("%w: the address has no port", ErrRefused)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || !allowedPorts[uint16(port)] {
		return "", 0, fmt.Errorf("%w: port not allowed", ErrRefused)
	}

	return host, uint16(port), nil
}

// GuardedDial returns the production DialFunc: a net.Dialer that checks, just before each connection is made, that
// the address it connects to is one allow accepts on an allowed port. The guard has already checked it; this
// checks the socket's own address, so no resolution or address family translation in between can reach another.
func GuardedDial(allow func(netip.Addr) bool) DialFunc {
	dialer := &net.Dialer{
		Timeout: connectTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil || !allowedPorts[addrPort.Port()] || !allow(addrPort.Addr()) {
				return fmt.Errorf("%w: address not allowed", ErrRefused)
			}
			return nil
		},
	}

	return dialer.DialContext
}
