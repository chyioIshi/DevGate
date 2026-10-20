package discovery

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestNewDNSResolverRejectsMissingIPResolver(t *testing.T) {
	t.Parallel()

	resolver, err := NewDNSResolver(nil)

	if err == nil {
		t.Fatal("NewDNSResolver() error = nil, want error")
	}
	if resolver != nil {
		t.Errorf("NewDNSResolver() resolver = %#v, want nil", resolver)
	}
	if err.Error() != "IP resolver was not provided" {
		t.Errorf("NewDNSResolver() error = %q, want missing resolver context", err)
	}
}

func TestDNSResolverResolveExpandsDistinctAddressesAndPreservesURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var gotNetwork string
	var gotHost string
	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		gotCtx context.Context,
		network string,
		host string,
	) ([]netip.Addr, error) {
		if gotCtx != ctx {
			t.Error("LookupNetIP() received a different context")
		}
		gotNetwork = network
		gotHost = host
		return []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
			netip.MustParseAddr("2001:db8::10"),
			netip.MustParseAddr("192.0.2.10"),
		}, nil
	}))
	target := url.URL{
		Scheme:      "http",
		User:        url.UserPassword("user", "password"),
		Host:        "service.example:8080",
		Path:        "/v1/users",
		RawPath:     "/v1/users",
		ForceQuery:  true,
		RawQuery:    "active=true",
		Fragment:    "details",
		RawFragment: "details",
	}

	got, err := resolver.Resolve(ctx, target)

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []url.URL{
		withDNSHost(target, "192.0.2.10:8080"),
		withDNSHost(target, "[2001:db8::10]:8080"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("Resolve() = %v, want %v", got, want)
	}
	if gotNetwork != "ip" {
		t.Errorf("LookupNetIP() network = %q, want %q", gotNetwork, "ip")
	}
	if gotHost != "service.example" {
		t.Errorf("LookupNetIP() host = %q, want %q", gotHost, "service.example")
	}
	if target.Host != "service.example:8080" {
		t.Errorf("Resolve() mutated input host to %q", target.Host)
	}
}

func TestDNSResolverResolveUsesDefaultHTTPPort(t *testing.T) {
	t.Parallel()

	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		context.Context,
		string,
		string,
	) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil
	}))
	target := url.URL{Scheme: "http", Host: "service.example"}

	got, err := resolver.Resolve(context.Background(), target)

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []url.URL{withDNSHost(target, "192.0.2.10:80")}
	if !slices.Equal(got, want) {
		t.Errorf("Resolve() = %v, want %v", got, want)
	}
}

func TestDNSResolverResolveAllowsEmptyResult(t *testing.T) {
	t.Parallel()

	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		context.Context,
		string,
		string,
	) ([]netip.Addr, error) {
		return nil, nil
	}))

	got, err := resolver.Resolve(
		context.Background(),
		url.URL{Scheme: "http", Host: "service.example"},
	)

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resolve() = %v, want empty", got)
	}
}

func TestDNSResolverResolveValidatesTargetBeforeLookup(t *testing.T) {
	t.Parallel()

	lookupCalled := false
	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		context.Context,
		string,
		string,
	) ([]netip.Addr, error) {
		lookupCalled = true
		return nil, nil
	}))
	tests := []struct {
		name        string
		target      url.URL
		wantMessage string
	}{
		{
			name:        "unsupported scheme",
			target:      url.URL{Scheme: "https", Host: "service.example"},
			wantMessage: "scheme",
		},
		{
			name:        "missing hostname",
			target:      url.URL{Scheme: "http"},
			wantMessage: "target hostname was not provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolver.Resolve(context.Background(), test.target)

			if err == nil {
				t.Fatal("Resolve() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Resolve() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
	if lookupCalled {
		t.Error("LookupNetIP() was called for an invalid target")
	}
}

func TestDNSResolverResolveReturnsCanceledContextBeforeLookup(t *testing.T) {
	t.Parallel()

	lookupCalled := false
	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		context.Context,
		string,
		string,
	) ([]netip.Addr, error) {
		lookupCalled = true
		return nil, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := resolver.Resolve(
		ctx,
		url.URL{Scheme: "http", Host: "service.example"},
	)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Resolve() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Errorf("Resolve() = %v, want nil", got)
	}
	if lookupCalled {
		t.Error("LookupNetIP() was called with an already canceled context")
	}
}

func TestDNSResolverResolveWrapsLookupError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("DNS unavailable")
	resolver := mustNewDNSResolver(t, ipResolverFunc(func(
		context.Context,
		string,
		string,
	) ([]netip.Addr, error) {
		return nil, wantErr
	}))

	got, err := resolver.Resolve(
		context.Background(),
		url.URL{Scheme: "http", Host: "service.example"},
	)

	if !errors.Is(err, wantErr) {
		t.Errorf("Resolve() error = %v, want wrapped lookup error", err)
	}
	if !strings.Contains(err.Error(), `hostname "service.example"`) {
		t.Errorf("Resolve() error = %q, want hostname context", err)
	}
	if got != nil {
		t.Errorf("Resolve() = %v, want nil", got)
	}
}

type ipResolverFunc func(
	context.Context,
	string,
	string,
) ([]netip.Addr, error)

func (f ipResolverFunc) LookupNetIP(
	ctx context.Context,
	network string,
	host string,
) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func mustNewDNSResolver(t *testing.T, resolver IPResolver) *DNSResolver {
	t.Helper()

	dnsResolver, err := NewDNSResolver(resolver)
	if err != nil {
		t.Fatalf("NewDNSResolver() error = %v", err)
	}
	return dnsResolver
}

func withDNSHost(target url.URL, host string) url.URL {
	target.Host = host
	return target
}
