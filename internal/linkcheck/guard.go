// Package linkcheck checks whether links work without letting a link reach anything but a public web server
// (docs/PLAN.md §5.5): every connection, on every redirect, goes to a global unicast address outside the
// special-purpose ranges, on port 80 or 443.
package linkcheck

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// ErrRefused is a URL or connection the guard refuses.
var ErrRefused = errors.New("refused by the link checker's guard")

// specialPurpose are the ranges IsGlobalUnicast accepts that are not public.
var specialPurpose = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // this network
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
	netip.MustParsePrefix("::/8"),            // unspecified, loopback, IPv4-compatible
	netip.MustParsePrefix("100::/64"),        // discard only
	netip.MustParsePrefix("2001::/23"),       // IETF protocol assignments, Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("3fff::/20"),       // documentation
	netip.MustParsePrefix("5f00::/16"),       // segment routing
	netip.MustParsePrefix("fc00::/7"),        // unique local
	netip.MustParsePrefix("fe80::/10"),       // link local
}

// translators are the IPv6 ranges that embed an IPv4 address: NAT64 (the last 32 bits) and 6to4 (the 32 after
// the prefix), which a connection could use to reach a private network.
var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

// publicAddress reports whether a link may reach addr.
func publicAddress(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	switch bytes := addr.As16(); {
	case nat64.Contains(addr):
		addr = netip.AddrFrom4([4]byte(bytes[12:16]))
	case sixToFour.Contains(addr):
		addr = netip.AddrFrom4([4]byte(bytes[2:6]))
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range specialPurpose {
		if prefix.Contains(addr) {
			return false
		}
	}

	return true
}

// checkAddress returns an error wrapping ErrRefused unless a link may connect to addr: a public address on port
// 80 or 443.
func checkAddress(addr netip.AddrPort) error {
	if (addr.Port() != 80 && addr.Port() != 443) || !publicAddress(addr.Addr()) {
		return fmt.Errorf("%w: address not allowed", ErrRefused)
	}

	return nil
}

// checkURL returns an error wrapping ErrRefused unless u is an http or https URL with a host and no credentials.
func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme not allowed", ErrRefused)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in the URL", ErrRefused)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: no host", ErrRefused)
	}

	return nil
}

// guardedDialer returns a dialer that checks each address it is about to connect to, after resolution, with
// check, so a host name that resolves to a private address cannot reach it.
func guardedDialer(check func(netip.AddrPort) error) *net.Dialer {
	return &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			addr, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: unreadable address", ErrRefused)
			}

			return check(addr)
		},
	}
}
