package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// composeNetwork is the production hearth network, the one range
// deploy/docker-compose.prod.yml trusts.
var composeNetwork = []netip.Prefix{netip.MustParsePrefix("172.28.0.0/16")}

// remoteAddrSeen runs one request through trustedProxyRealIP and reports the
// r.RemoteAddr the next handler saw.
func remoteAddrSeen(t *testing.T, trusted []netip.Prefix, remoteAddr string, headers map[string]string) string {
	t.Helper()
	var seen string
	handler := trustedProxyRealIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

// The attack the middleware exists to stop: a caller reaching the API
// directly names its own address to get a fresh rate-limit bucket.
func TestRealIPIgnoresXRealIPFromAnUntrustedPeer(t *testing.T) {
	got := remoteAddrSeen(t, composeNetwork, "203.0.113.9:5555", map[string]string{"X-Real-IP": "198.51.100.7"})
	if got != "203.0.113.9:5555" {
		t.Fatalf("RemoteAddr = %q, want the untouched peer 203.0.113.9:5555", got)
	}
}

func TestRealIPTakesXRealIPFromATrustedPeer(t *testing.T) {
	got := remoteAddrSeen(t, composeNetwork, "172.28.0.4:40000", map[string]string{"X-Real-IP": "198.51.100.7"})
	if got != "198.51.100.7" {
		t.Fatalf("RemoteAddr = %q, want nginx's X-Real-IP 198.51.100.7", got)
	}
}

func TestRealIPKeepsThePeerWhenATrustedProxySendsNoUsableHeader(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"no header":     nil,
		"not an IP":     {"X-Real-IP": "not-an-ip"},
		"empty header":  {"X-Real-IP": ""},
		"host and port": {"X-Real-IP": "198.51.100.7:80"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := remoteAddrSeen(t, composeNetwork, "172.28.0.4:40000", headers); got != "172.28.0.4:40000" {
				t.Fatalf("RemoteAddr = %q, want the untouched peer", got)
			}
		})
	}
}

// chi's RealIP read both of these, ahead of and after X-Real-IP. Neither is
// read at all now, from a trusted peer or anyone else.
func TestRealIPNeverReadsTrueClientIPOrXForwardedFor(t *testing.T) {
	spoofed := map[string]string{"True-Client-IP": "198.51.100.7", "X-Forwarded-For": "198.51.100.8"}
	for _, peer := range []string{"172.28.0.4:40000", "203.0.113.9:5555"} {
		if got := remoteAddrSeen(t, composeNetwork, peer, spoofed); got != peer {
			t.Fatalf("peer %s: RemoteAddr = %q, want it untouched", peer, got)
		}
	}
}

func TestRealIPTrustsNobodyWhenNoProxiesAreConfigured(t *testing.T) {
	got := remoteAddrSeen(t, nil, "127.0.0.1:1234", map[string]string{"X-Real-IP": "198.51.100.7"})
	if got != "127.0.0.1:1234" {
		t.Fatalf("RemoteAddr = %q, want the untouched peer", got)
	}
}

// An IPv4 peer accepted on an IPv6 socket is written ::ffff:a.b.c.d; without
// unmapping it, the IPv4 prefix would never match and nginx would silently
// become one shared bucket.
func TestRealIPMatchesAnIPv4MappedPeerAgainstAnIPv4Prefix(t *testing.T) {
	got := remoteAddrSeen(t, composeNetwork, "[::ffff:172.28.0.4]:40000", map[string]string{"X-Real-IP": "198.51.100.7"})
	if got != "198.51.100.7" {
		t.Fatalf("RemoteAddr = %q, want nginx's X-Real-IP", got)
	}
}
