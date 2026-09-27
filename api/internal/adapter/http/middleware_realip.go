package httpadapter

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// trustedProxyRealIP sets r.RemoteAddr to the X-Real-IP header's address,
// but only when the request's TCP peer is inside trusted (in production,
// TRUSTED_PROXY_CIDRS is the Docker network nginx reaches the API from).
// Everything downstream reads the client's address via clientIP, which
// reads r.RemoteAddr, so this is the one place that decides whose word to
// believe.
//
// It replaces chi's middleware.RealIP, which believed True-Client-IP,
// X-Real-IP and X-Forwarded-For from anyone -- making the API only as safe
// as whatever sat in front of it, letting a caller walk past the sign-up
// limiter by naming its own address whenever nginx wasn't configured
// exactly right, or wasn't there at all. This reads one header, from
// listed peers only, and never True-Client-IP or X-Forwarded-For, since
// nginx already overwrites X-Real-IP with the client it resolved -- one
// header to trust, no list to walk.
//
// With trusted empty, nothing is rewritten and every request is keyed by
// the address that actually connected (config.Load's own comment explains
// why that is the default). No cleverness: a peer outside the list, a
// missing header, or a header that isn't an address all leave the request
// untouched.
func trustedProxyRealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client, ok := addressFromTrustedProxy(r, trusted); ok {
				r.RemoteAddr = client
			}
			next.ServeHTTP(w, r)
		})
	}
}

// addressFromTrustedProxy returns the X-Real-IP address when, and only when,
// the peer that sent it is trusted and the header holds a valid address. The
// result is a bare address with no port, the same shape chi's RealIP left in
// r.RemoteAddr, so clientIP and the audit log read both cases alike.
func addressFromTrustedProxy(r *http.Request, trusted []netip.Prefix) (string, bool) {
	if len(trusted) == 0 {
		return "", false
	}
	peer, ok := peerAddr(r.RemoteAddr)
	if !ok || !anyContains(trusted, peer) {
		return "", false
	}
	claimed, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP")))
	if err != nil {
		return "", false
	}
	return claimed.Unmap().String(), true
}

// peerAddr parses r.RemoteAddr, which net/http sets to "host:port" for a
// real connection. An IPv4 peer that reached an IPv6 socket arrives as
// ::ffff:a.b.c.d, and Unmap is what lets it match an IPv4 prefix.
func peerAddr(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func anyContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
