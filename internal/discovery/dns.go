package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
)

// IPResolver resolves hostnames to IP addresses. net.Resolver satisfies this
// interface.
type IPResolver interface {
	LookupNetIP(
		ctx context.Context,
		network string,
		host string,
	) ([]netip.Addr, error)
}

// DNSResolver expands an HTTP target hostname into concrete IP-based target
// URLs.
type DNSResolver struct {
	resolver IPResolver
}

// NewDNSResolver creates a DNS resolver from an explicit IP resolver.
func NewDNSResolver(resolver IPResolver) (*DNSResolver, error) {
	if resolver == nil {
		return nil, errors.New("IP resolver was not provided")
	}
	return &DNSResolver{
		resolver: resolver,
	}, nil
}

// Resolve returns one copied target URL per distinct resolved IP address while
// preserving resolver order. An empty DNS result is returned without error.
func (r *DNSResolver) Resolve(
	ctx context.Context,
	target url.URL,
) ([]url.URL, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if target.Scheme != "http" {
		return nil, fmt.Errorf(
			"target's scheme for resolve must be http, got %q",
			target.Scheme,
		)
	}
	hostname := target.Hostname()
	if hostname == "" {
		return nil, fmt.Errorf("target hostname was not provided")
	}

	addrs, err := r.resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve hostname %q: %w", hostname, err)
	}
	if len(addrs) == 0 {
		return nil, nil
	}

	urls := make([]url.URL, 0, len(addrs))
	seen := make(map[netip.Addr]struct{})
	for _, addr := range addrs {
		if _, ok := seen[addr]; ok {
			continue
		}
		targetClone := target
		targetPort := targetClone.Port()
		if targetPort == "" {
			targetPort = "80"
		}

		targetClone.Host = net.JoinHostPort(addr.String(), targetPort)
		seen[addr] = struct{}{}
		urls = append(urls, targetClone)
	}
	return urls, nil
}
