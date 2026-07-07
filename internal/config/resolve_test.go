package config

import (
	"net"
	"strings"
	"testing"
)

func TestResolvePublicHost_Explicit(t *testing.T) {
	// Anything that is not the wildcard address is returned verbatim.
	cases := []string{
		"127.0.0.1",
		"192.168.1.10",
		"warehouse.example.com",
		"::1",
		"fe80::1",
	}
	for _, in := range cases {
		if got := ResolvePublicHost(in); got != in {
			t.Errorf("ResolvePublicHost(%q) = %q, want %q", in, got, in)
		}
	}
}

func TestResolvePublicHost_WildcardToLocal(t *testing.T) {
	// 0.0.0.0 / "::" / "" should resolve to a real local IP if the host
	// has any usable interface, otherwise to "localhost".
	for _, in := range []string{"", "0.0.0.0", "::"} {
		got := ResolvePublicHost(in)
		if got == "0.0.0.0" || got == "::" {
			t.Errorf("ResolvePublicHost(%q) returned wildcard %q; expected an address or localhost", in, got)
		}
		if got == "" {
			t.Errorf("ResolvePublicHost(%q) returned empty; expected an address or localhost", in)
		}
	}
}

func TestResolvePublicHost_FallbackToLocalhost(t *testing.T) {
	// Simulate "no network": point firstLocalIPv4 at an empty list.
	orig := firstLocalIPv4
	firstLocalIPv4 = func() string { return "" }
	defer func() { firstLocalIPv4 = orig }()

	if got := ResolvePublicHost("0.0.0.0"); got != "localhost" {
		t.Errorf("with no interfaces, ResolvePublicHost(0.0.0.0) = %q, want %q", got, "localhost")
	}
}

func TestFirstLocalIPv4_SkipsLoopback(t *testing.T) {
	// The CI/sandbox env may have only loopback; we just assert we never
	// return 127.x.x.x or an empty loopback.
	ip := firstLocalIPv4()
	if ip == "" {
		return // acceptable: no interfaces
	}
	if strings.HasPrefix(ip, "127.") {
		t.Errorf("firstLocalIPv4 returned loopback %q", ip)
	}
	if net.ParseIP(ip).To4() == nil {
		t.Errorf("firstLocalIPv4 returned non-IPv4 %q", ip)
	}
}
