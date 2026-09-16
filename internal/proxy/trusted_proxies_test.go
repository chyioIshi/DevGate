package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestSetXForwardedFor(t *testing.T) {
	trustedCIDRs := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}

	tests := []struct {
		name          string
		remoteAddr    string
		forwardedFor  []string
		wantForwarded string
	}{
		{
			name:          "invalid remote address removes outbound header",
			remoteAddr:    "invalid",
			forwardedFor:  []string{"198.51.100.20"},
			wantForwarded: "",
		},
		{
			name:          "untrusted peer discards incoming chain",
			remoteAddr:    "203.0.113.10:1234",
			forwardedFor:  []string{"198.51.100.20"},
			wantForwarded: "203.0.113.10",
		},
		{
			name:          "untrusted peer ignores malformed incoming chain",
			remoteAddr:    "203.0.113.10:1234",
			forwardedFor:  []string{"not-an-address"},
			wantForwarded: "203.0.113.10",
		},
		{
			name:          "trusted peer without incoming chain",
			remoteAddr:    "10.0.0.2:1234",
			wantForwarded: "10.0.0.2",
		},
		{
			name:       "trusted peer preserves chain from client boundary",
			remoteAddr: "10.0.0.2:1234",
			forwardedFor: []string{
				"198.51.100.20, 203.0.113.10, 10.0.0.1",
			},
			wantForwarded: "203.0.113.10, 10.0.0.1, 10.0.0.2",
		},
		{
			name:          "malformed chain from trusted peer falls back to peer",
			remoteAddr:    "10.0.0.2:1234",
			forwardedFor:  []string{"203.0.113.10, invalid"},
			wantForwarded: "10.0.0.2",
		},
		{
			name:       "multiple header lines preserve order",
			remoteAddr: "10.0.0.2:1234",
			forwardedFor: []string{
				"203.0.113.10",
				"10.0.0.1",
			},
			wantForwarded: "203.0.113.10, 10.0.0.1, 10.0.0.2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := httptest.NewRequest(http.MethodGet, "http://gateway.example/resource", nil)
			in.RemoteAddr = test.remoteAddr
			for _, value := range test.forwardedFor {
				in.Header.Add("X-Forwarded-For", value)
			}
			originalInput := slices.Clone(in.Header.Values("X-Forwarded-For"))

			out := in.Clone(in.Context())
			out.Header.Set("X-Forwarded-For", "stale-outbound-value")
			pr := &httputil.ProxyRequest{In: in, Out: out}

			setXForwardedFor(pr, trustedCIDRs)

			if got := out.Header.Get("X-Forwarded-For"); got != test.wantForwarded {
				t.Errorf("outbound X-Forwarded-For = %q, want %q", got, test.wantForwarded)
			}
			if test.wantForwarded == "" && len(out.Header.Values("X-Forwarded-For")) != 0 {
				t.Errorf("outbound X-Forwarded-For values = %q, want header removed", out.Header.Values("X-Forwarded-For"))
			}
			if got := in.Header.Values("X-Forwarded-For"); !slices.Equal(got, originalInput) {
				t.Errorf("inbound X-Forwarded-For = %q, want unchanged %q", got, originalInput)
			}
		})
	}
}

func TestFormatXForwardedFor(t *testing.T) {
	atLimit := make([]netip.Addr, maxForwardedForHops+1)
	for i := range atLimit {
		atLimit[i] = netip.MustParseAddr("192.0.2.10")
	}
	aboveLimit := append(slices.Clone(atLimit), netip.MustParseAddr("192.0.2.11"))

	tests := []struct {
		name      string
		addresses []netip.Addr
		want      string
		wantOK    bool
	}{
		{
			name:   "empty chain",
			wantOK: true,
		},
		{
			name:      "single address",
			addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")},
			want:      "192.0.2.10",
			wantOK:    true,
		},
		{
			name: "preserves IPv4 and IPv6 order",
			addresses: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("2001:db8::10"),
				netip.MustParseAddr("10.0.0.1"),
			},
			want:   "203.0.113.10, 2001:db8::10, 10.0.0.1",
			wantOK: true,
		},
		{
			name:      "unmaps mapped IPv4",
			addresses: []netip.Addr{netip.MustParseAddr("::ffff:192.0.2.10")},
			want:      "192.0.2.10",
			wantOK:    true,
		},
		{
			name:      "invalid address",
			addresses: []netip.Addr{{}},
		},
		{
			name:      "zoned IPv6 address",
			addresses: []netip.Addr{netip.MustParseAddr("fe80::1%en0")},
		},
		{
			name:      "zoned mapped IPv4 address",
			addresses: []netip.Addr{netip.MustParseAddr("::ffff:192.0.2.10%en0")},
		},
		{
			name:      "at limit including immediate peer",
			addresses: atLimit,
			want:      strings.TrimSuffix(strings.Repeat("192.0.2.10, ", maxForwardedForHops+1), ", "),
			wantOK:    true,
		},
		{
			name:      "above limit",
			addresses: aboveLimit,
		},
		{
			name: "invalid suffix returns no partial result",
			addresses: []netip.Addr{
				netip.MustParseAddr("192.0.2.10"),
				{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := formatXForwardedFor(test.addresses)
			if ok != test.wantOK {
				t.Errorf("formatXForwardedFor(%v) ok = %t, want %t", test.addresses, ok, test.wantOK)
			}
			if got != test.want {
				t.Errorf("formatXForwardedFor(%v) = %q, want %q", test.addresses, got, test.want)
			}
		})
	}
}

func TestSanitizeXForwardedFor(t *testing.T) {
	trustedCIDRs := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	tooManyHops := make([]netip.Addr, maxForwardedForHops+1)
	for i := range tooManyHops {
		tooManyHops[i] = netip.MustParseAddr("10.0.0.1")
	}

	tests := []struct {
		name         string
		remoteIP     netip.Addr
		forwardedFor []netip.Addr
		trustedCIDRs []netip.Prefix
		want         []netip.Addr
	}{
		{
			name:         "invalid remote address",
			trustedCIDRs: trustedCIDRs,
		},
		{
			name:     "untrusted peer discards incoming chain",
			remoteIP: netip.MustParseAddr("203.0.113.10"),
			forwardedFor: []netip.Addr{
				netip.MustParseAddr("198.51.100.20"),
			},
			trustedCIDRs: trustedCIDRs,
			want:         []netip.Addr{netip.MustParseAddr("203.0.113.10")},
		},
		{
			name:         "trusted peer without incoming chain",
			remoteIP:     netip.MustParseAddr("10.0.0.2"),
			trustedCIDRs: trustedCIDRs,
			want:         []netip.Addr{netip.MustParseAddr("10.0.0.2")},
		},
		{
			name:     "trusted peer preserves client boundary",
			remoteIP: netip.MustParseAddr("10.0.0.2"),
			forwardedFor: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
			},
			trustedCIDRs: trustedCIDRs,
			want: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.2"),
			},
		},
		{
			name:     "spoofed addresses left of client boundary are discarded",
			remoteIP: netip.MustParseAddr("10.0.0.2"),
			forwardedFor: []netip.Addr{
				netip.MustParseAddr("198.51.100.20"),
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
			},
			trustedCIDRs: trustedCIDRs,
			want: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("10.0.0.2"),
			},
		},
		{
			name:     "all trusted addresses are preserved",
			remoteIP: netip.MustParseAddr("10.0.0.2"),
			forwardedFor: []netip.Addr{
				netip.MustParseAddr("10.0.0.1"),
			},
			trustedCIDRs: trustedCIDRs,
			want: []netip.Addr{
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("10.0.0.2"),
			},
		},
		{
			name:     "invalid address in trusted suffix falls back to peer",
			remoteIP: netip.MustParseAddr("10.0.0.2"),
			forwardedFor: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				{},
			},
			trustedCIDRs: trustedCIDRs,
			want:         []netip.Addr{netip.MustParseAddr("10.0.0.2")},
		},
		{
			name:     "invalid address left of client boundary is discarded",
			remoteIP: netip.MustParseAddr("10.0.0.2"),
			forwardedFor: []netip.Addr{
				{},
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
			},
			trustedCIDRs: trustedCIDRs,
			want: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("10.0.0.2"),
			},
		},
		{
			name:         "mapped peer is normalized",
			remoteIP:     netip.MustParseAddr("::ffff:10.0.0.2"),
			trustedCIDRs: trustedCIDRs,
			want:         []netip.Addr{netip.MustParseAddr("10.0.0.2")},
		},
		{
			name:         "chain above limit falls back to peer",
			remoteIP:     netip.MustParseAddr("10.0.0.2"),
			forwardedFor: tooManyHops,
			trustedCIDRs: trustedCIDRs,
			want:         []netip.Addr{netip.MustParseAddr("10.0.0.2")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeXForwardedFor(test.remoteIP, test.forwardedFor, test.trustedCIDRs)
			if !slices.Equal(got, test.want) {
				t.Errorf("sanitizeXForwardedFor() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSanitizeXForwardedForDoesNotMutateOrAliasInput(t *testing.T) {
	forwardedFor := []netip.Addr{
		netip.MustParseAddr("203.0.113.10"),
		netip.MustParseAddr("10.0.0.1"),
	}
	wantInput := slices.Clone(forwardedFor)

	got := sanitizeXForwardedFor(
		netip.MustParseAddr("10.0.0.2"),
		forwardedFor,
		[]netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
	)

	if !slices.Equal(forwardedFor, wantInput) {
		t.Errorf("forwarded chain after sanitization = %v, want unchanged %v", forwardedFor, wantInput)
	}
	got[0] = netip.MustParseAddr("192.0.2.99")
	if !slices.Equal(forwardedFor, wantInput) {
		t.Errorf("forwarded chain after result mutation = %v, want unchanged %v", forwardedFor, wantInput)
	}
}

func TestParseXForwardedFor(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []netip.Addr
		wantOK bool
	}{
		{
			name:   "header absent",
			wantOK: true,
		},
		{
			name:   "single IPv4 address",
			values: []string{"192.0.2.10"},
			want:   []netip.Addr{netip.MustParseAddr("192.0.2.10")},
			wantOK: true,
		},
		{
			name:   "comma-separated addresses with whitespace",
			values: []string{" 203.0.113.10, 10.0.0.1 ,10.0.0.2 "},
			want: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("10.0.0.2"),
			},
			wantOK: true,
		},
		{
			name:   "multiple header lines preserve order",
			values: []string{"203.0.113.10, 10.0.0.1", "10.0.0.2"},
			want: []netip.Addr{
				netip.MustParseAddr("203.0.113.10"),
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("10.0.0.2"),
			},
			wantOK: true,
		},
		{
			name:   "IPv6 address",
			values: []string{"2001:db8::10"},
			want:   []netip.Addr{netip.MustParseAddr("2001:db8::10")},
			wantOK: true,
		},
		{
			name:   "mapped IPv4 is unmapped",
			values: []string{"::ffff:192.0.2.10"},
			want:   []netip.Addr{netip.MustParseAddr("192.0.2.10")},
			wantOK: true,
		},
		{
			name:   "hostname",
			values: []string{"client.example"},
		},
		{
			name:   "address with port",
			values: []string{"192.0.2.10:1234"},
		},
		{
			name:   "address with zone",
			values: []string{"fe80::1%en0"},
		},
		{
			name:   "empty element",
			values: []string{"203.0.113.10,,10.0.0.1"},
		},
		{
			name:   "invalid element returns no partial chain",
			values: []string{"203.0.113.10,invalid"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseXForwardedFor(test.values)
			if ok != test.wantOK {
				t.Errorf("parseXForwardedFor(%q) ok = %t, want %t", test.values, ok, test.wantOK)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("parseXForwardedFor(%q) = %v, want %v", test.values, got, test.want)
			}
		})
	}
}

func TestParseXForwardedForHopLimit(t *testing.T) {
	for _, test := range []struct {
		name   string
		hops   int
		wantOK bool
	}{
		{name: "at limit", hops: maxForwardedForHops, wantOK: true},
		{name: "above limit", hops: maxForwardedForHops + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := make([]string, test.hops)
			for i := range values {
				values[i] = "192.0.2.10"
			}

			got, ok := parseXForwardedFor(values)
			if ok != test.wantOK {
				t.Errorf("parseXForwardedFor() ok = %t, want %t", ok, test.wantOK)
			}
			if test.wantOK && len(got) != test.hops {
				t.Errorf("parseXForwardedFor() addresses = %d, want %d", len(got), test.hops)
			}
			if !test.wantOK && got != nil {
				t.Errorf("parseXForwardedFor() = %v, want nil after limit error", got)
			}
		})
	}
}

func TestParseXForwardedForHopLimitWithinSingleHeader(t *testing.T) {
	values := []string{strings.Repeat("192.0.2.10,", maxForwardedForHops) + "192.0.2.10"}

	got, ok := parseXForwardedFor(values)
	if ok {
		t.Fatal("parseXForwardedFor() ok = true, want hop limit error")
	}
	if got != nil {
		t.Errorf("parseXForwardedFor() = %v, want nil after limit error", got)
	}
}

func TestParseRemoteIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       netip.Addr
		wantOK     bool
	}{
		{
			name:       "IPv4",
			remoteAddr: "192.0.2.10:54321",
			want:       netip.MustParseAddr("192.0.2.10"),
			wantOK:     true,
		},
		{
			name:       "IPv6",
			remoteAddr: "[2001:db8::10]:54321",
			want:       netip.MustParseAddr("2001:db8::10"),
			wantOK:     true,
		},
		{
			name:       "IPv6 zone removed",
			remoteAddr: "[fe80::1%en0]:443",
			want:       netip.MustParseAddr("fe80::1"),
			wantOK:     true,
		},
		{
			name:       "mapped IPv4 remains mapped",
			remoteAddr: "[::ffff:192.0.2.10]:54321",
			want:       netip.MustParseAddr("::ffff:192.0.2.10"),
			wantOK:     true,
		},
		{
			name: "empty address",
		},
		{
			name:       "hostname",
			remoteAddr: "proxy.internal:54321",
		},
		{
			name:       "bare IPv4 address",
			remoteAddr: "192.0.2.10",
		},
		{
			name:       "bare IPv6 address",
			remoteAddr: "2001:db8::10",
		},
		{
			name:       "non-numeric port",
			remoteAddr: "192.0.2.10:http",
		},
		{
			name:       "port above range",
			remoteAddr: "192.0.2.10:65536",
		},
		{
			name:       "zero port",
			remoteAddr: "192.0.2.10:0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseRemoteIP(test.remoteAddr)
			if ok != test.wantOK {
				t.Errorf("parseRemoteIP(%q) ok = %t, want %t", test.remoteAddr, ok, test.wantOK)
			}
			if got != test.want {
				t.Errorf("parseRemoteIP(%q) address = %v, want %v", test.remoteAddr, got, test.want)
			}
		})
	}
}

func TestIsTrustedProxy(t *testing.T) {
	tests := []struct {
		name         string
		addr         netip.Addr
		trustedCIDRs []netip.Prefix
		want         bool
	}{
		{
			name: "invalid address",
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/8"),
			},
		},
		{
			name: "empty trusted networks",
			addr: netip.MustParseAddr("10.0.0.10"),
		},
		{
			name:         "invalid prefix",
			addr:         netip.MustParseAddr("10.0.0.10"),
			trustedCIDRs: []netip.Prefix{{}},
		},
		{
			name: "trusted IPv4 address",
			addr: netip.MustParseAddr("10.0.0.10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/24"),
			},
			want: true,
		},
		{
			name: "untrusted IPv4 address",
			addr: netip.MustParseAddr("10.0.1.10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/24"),
			},
		},
		{
			name: "trusted IPv6 address",
			addr: netip.MustParseAddr("2001:db8::10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("2001:db8::/64"),
			},
			want: true,
		},
		{
			name: "untrusted IPv6 address",
			addr: netip.MustParseAddr("2001:db8:1::10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("2001:db8::/64"),
			},
		},
		{
			name: "mapped IPv4 address in IPv4 network",
			addr: netip.MustParseAddr("::ffff:10.0.0.10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/24"),
			},
			want: true,
		},
		{
			name: "mapped IPv4 address in mapped network",
			addr: netip.MustParseAddr("::ffff:10.0.0.10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("::ffff:10.0.0.0/120"),
			},
			want: true,
		},
		{
			name: "matches later network",
			addr: netip.MustParseAddr("192.0.2.10"),
			trustedCIDRs: []netip.Prefix{
				netip.MustParsePrefix("10.0.0.0/8"),
				netip.MustParsePrefix("192.0.2.0/24"),
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isTrustedProxy(test.addr, test.trustedCIDRs); got != test.want {
				t.Errorf("isTrustedProxy(%v, %v) = %t, want %t", test.addr, test.trustedCIDRs, got, test.want)
			}
		})
	}
}

func TestIsTrustedProxyDoesNotMutatePrefixes(t *testing.T) {
	trustedCIDRs := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	want := slices.Clone(trustedCIDRs)

	isTrustedProxy(netip.MustParseAddr("10.0.0.10"), trustedCIDRs)

	if !slices.Equal(trustedCIDRs, want) {
		t.Errorf("trusted CIDRs after lookup = %v, want unchanged %v", trustedCIDRs, want)
	}
}
