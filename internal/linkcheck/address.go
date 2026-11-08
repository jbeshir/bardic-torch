// Package linkcheck checks whether links work without letting a link reach anything but a public web server: every
// connection, on every redirect, goes to an address the allowlist accepts, on port 80 or 443 (docs/PLAN.md §3,
// "cmd/check-links").
package linkcheck

import (
	"net/netip"
)

// specialPurpose are the ranges of the IANA special-purpose address registries, and the other ranges that are not
// public unicast, that IsGlobalUnicast accepts. A link may not reach any of them.
var specialPurpose = []netip.Prefix{
	// IPv4.
	netip.MustParsePrefix("0.0.0.0/8"),          // this network
	netip.MustParsePrefix("10.0.0.0/8"),         // private
	netip.MustParsePrefix("100.64.0.0/10"),      // shared address space (carrier-grade NAT)
	netip.MustParsePrefix("127.0.0.0/8"),        // loopback
	netip.MustParsePrefix("169.254.0.0/16"),     // link local
	netip.MustParsePrefix("172.16.0.0/12"),      // private
	netip.MustParsePrefix("192.0.0.0/24"),       // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),       // documentation
	netip.MustParsePrefix("192.88.99.0/24"),     // 6to4 relay anycast
	netip.MustParsePrefix("192.168.0.0/16"),     // private
	netip.MustParsePrefix("198.18.0.0/15"),      // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"),    // documentation
	netip.MustParsePrefix("203.0.113.0/24"),     // documentation
	netip.MustParsePrefix("224.0.0.0/4"),        // multicast
	netip.MustParsePrefix("240.0.0.0/4"),        // reserved, and the limited broadcast address
	netip.MustParsePrefix("255.255.255.255/32"), // limited broadcast
	// IPv6.
	netip.MustParsePrefix("::/8"),           // reserved: unspecified, loopback, IPv4-compatible
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use IPv4/IPv6 translation
	netip.MustParsePrefix("100::/64"),       // discard only
	netip.MustParsePrefix("2001::/23"),      // IETF protocol assignments, Teredo among them
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
	netip.MustParsePrefix("3fff::/20"),      // documentation
	netip.MustParsePrefix("5f00::/16"),      // segment routing
	netip.MustParsePrefix("fc00::/7"),       // unique local
	netip.MustParsePrefix("fe80::/10"),      // link local
	netip.MustParsePrefix("ff00::/8"),       // multicast
}

// The IPv6 ranges that embed an IPv4 address, which a connection to them may reach.
var (
	// nat64 is the well-known NAT64 prefix: the IPv4 address is the last 32 bits.
	nat64 = netip.MustParsePrefix("64:ff9b::/96")
	// sixToFour is 6to4: the IPv4 address is the 32 bits after the prefix.
	sixToFour = netip.MustParsePrefix("2002::/16")
)

// PublicAddress reports whether a link may reach addr: a global unicast address outside every special-purpose
// range. An IPv4-mapped address is judged as its IPv4 address, and a NAT64 or 6to4 address by the IPv4 address it
// embeds, so neither can reach a private network through a translator.
func PublicAddress(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	if embedded, ok := embeddedIPv4(addr); ok {
		addr = embedded
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

// embeddedIPv4 returns the IPv4 address a NAT64 or 6to4 address embeds.
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	bytes := addr.As16()
	switch {
	case !addr.Is6():
		return netip.Addr{}, false
	case nat64.Contains(addr):
		return netip.AddrFrom4([4]byte(bytes[12:16])), true
	case sixToFour.Contains(addr):
		return netip.AddrFrom4([4]byte(bytes[2:6])), true
	default:
		return netip.Addr{}, false
	}
}
