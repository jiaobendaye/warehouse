package config

import "net"

// ResolvePublicHost returns a public-facing host that external clients (MCP
// agents, browsers on the LAN) can use to reach this server.
//
// Resolution rules:
//
//	cfgHost == "0.0.0.0" / "::" / "" → enumerate local network interfaces,
//	                                   pick the first UP+RUNNING non-loopback
//	                                   IPv4 address
//	otherwise                          → return cfgHost verbatim
//
// Falls back to "localhost" if no usable interface is found. The IP detection
// happens once at startup so every URL the server emits (export downloads,
// health pings) points at an address that is actually reachable from outside
// the loopback.
func ResolvePublicHost(cfgHost string) string {
	if cfgHost != "" && cfgHost != "0.0.0.0" && cfgHost != "::" {
		return cfgHost
	}
	if ip := firstLocalIPv4(); ip != "" {
		return ip
	}
	return "localhost"
}

// firstLocalIPv4 is a package-level hook so tests can stub the network
// probe. The default walks net.Interfaces() and returns the first IPv4
// address on an UP, RUNNING, non-loopback interface. Returns "" if nothing
// matches (no network, broken netlink, etc.) so the caller can fall back.
var firstLocalIPv4 = func() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		// Skip interfaces that are down, not running, or loopback.
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagRunning == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := extractIPv4(addr)
			if ip == "" {
				continue
			}
			return ip
		}
	}
	return ""
}

// extractIPv4 returns the dotted-quad string of addr if it carries a
// non-loopback IPv4 address, otherwise "". Handles both *net.IPNet (most
// common from Interface.Addrs) and *net.IPAddr.
func extractIPv4(addr net.Addr) string {
	var ip net.IP
	switch v := addr.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	default:
		return ""
	}
	if ip == nil {
		return ""
	}
	ip = ip.To4()
	if ip == nil || ip.IsLoopback() {
		return ""
	}
	return ip.String()
}
